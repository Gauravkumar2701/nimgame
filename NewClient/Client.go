package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/DistributedClocks/tracing"
)

/* Config struct */

type ClientConfig struct {
	ClientAddress        string
	NimServerAddress     string
	TracingServerAddress string
	Secret               []byte
	TracingIdentity      string
}

/* Tracing structs */

type GameStart struct {
	Seed int8
}

type ClientMove StateMoveMessage

type ServerMoveReceive StateMoveMessage

type GameComplete struct {
	Winner string
}

/* Message structs */

type StateMoveMessage struct {
	GameState []uint8
	MoveRow   int8
	MoveCount int8
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: client.go [seed]")
		return
	}
	arg, err := strconv.Atoi(os.Args[1])
	CheckErr(err, "Provided seed could not be converted to integer", arg)
	seed := int8(arg)

	config := ReadConfig("config/client_config.json")

	// now connect to it
	tracer := tracing.NewTracer(tracing.TracerConfig{
		ServerAddress:  config.TracingServerAddress,
		TracerIdentity: config.TracingIdentity,
		Secret:         config.Secret,
	})
	defer tracer.Close()

	trace := tracer.CreateTrace()
	trace.RecordAction(
		GameStart{
			Seed: seed,
		})

	local_ip_port := config.ClientAddress
	remote_ip_port := config.NimServerAddress

	laddr, err := net.ResolveUDPAddr("udp", local_ip_port)
	CheckErr(err, "Error converting UDP address: %v\n", err)
	raddr, err := net.ResolveUDPAddr("udp", remote_ip_port)
	CheckErr(err, "Error converting UDP address: %v\n", err)

	// setup UDP connection
	conn, err := net.DialUDP("udp", laddr, raddr)
	CheckErr(err, "Couldn't connect to the server", config.NimServerAddress)
	defer conn.Close()

	// get board state
	sendMove := StateMoveMessage{nil, -1, seed}
	var recvMove StateMoveMessage
	for {
		// send start packet
		traceAndSend(&sendMove, trace, conn)

		// get server response
		if recvAndTrace(&recvMove, trace, conn) != nil {
			continue
		}
		break

	}
	state := make([]uint8, len(recvMove.GameState))
	copy(state, recvMove.GameState)

	// main loop
	for {
		// make move and update state
		sendMove = decideMove(state)
		copy(state, sendMove.GameState)
		for {
			// send my move
			traceAndSend(&sendMove, trace, conn)

			// if I won, stop
			if isWinState(state) {
				trace.RecordAction(GameComplete{"client"})
				os.Exit(0)
			}

			// get server response
			if recvAndTrace(&recvMove, trace, conn) != nil {
				fmt.Fprintln(os.Stderr, "saw timeout or corrupt packet")
				continue
			} else if !isValidSuccessor(state, &recvMove) {
				fmt.Fprintln(os.Stderr, "saw invalid/duplicate (but not corrupt) packet")
				fmt.Fprintln(os.Stderr, "state = ", state, " received = ", recvMove.GameState)
				continue
			}
			break
		}
		copy(state, recvMove.GameState)
		// if server won, stop
		if isWinState(state) {
			trace.RecordAction(GameComplete{"server"})
			os.Exit(0)
		}
	}
}

func decideMove(state []uint8) StateMoveMessage {
	// winning nim strategy as described by https://en.wikipedia.org/wiki/Nim
	var nimSum uint8
	for _, elm := range state {
		nimSum ^= elm
	}

	if nimSum != 0 {
		for idx, elm := range state {
			if elm >= elm^nimSum {
				reduceBy := elm - (elm ^ nimSum)
				newState := make([]uint8, len(state))
				copy(newState, state)
				newState[idx] -= reduceBy
				return StateMoveMessage{newState, int8(idx), int8(reduceBy)}
			}
		}
	} else {
		for idx, elm := range state {
			if elm != 0 {
				newState := make([]uint8, len(state))
				copy(newState, state)
				newState[idx] -= 1
				return StateMoveMessage{newState, int8(idx), 1}
			}
		}
	}

	fmt.Fprintln(os.Stderr, "move decision strategy failed")
	fmt.Fprintln(os.Stderr, "state = ", state)
	os.Exit(1)
	return StateMoveMessage{}
}

func isWinState(state []uint8) bool {
	for _, elm := range state {
		if elm != 0 {
			return false
		}
	}
	return true
}

func isValidSuccessor(state []uint8, move *StateMoveMessage) bool {
	for idx, elm := range state {
		if idx == int(move.MoveRow) {
			if elm-uint8(move.MoveCount) != move.GameState[idx] {
				return false
			}
		} else {
			if elm != move.GameState[idx] {
				return false
			}
		}
	}

	return true
}

func traceAndSend(move *StateMoveMessage, trace *tracing.Trace, conn net.Conn) {
	trace.RecordAction(ClientMove(*move))
	conn.Write(encode(move))
	// assume it went through, if it didn't, we'll just retry after a timeout
}

func recvAndTrace(move *StateMoveMessage, trace *tracing.Trace, conn net.Conn) error {
	recvBuf := make([]byte, 1024)

	conn.SetReadDeadline(time.Now().Add(time.Duration(1) * time.Second))
	len, err := conn.Read(recvBuf)
	if err != nil {
		return err
	}
	decoded, err := decode(recvBuf, len)
	if err != nil {
		return err
	}
	*move = decoded
	trace.RecordAction(ServerMoveReceive(*move))
	return nil
}

func encode(move *StateMoveMessage) []byte {
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(move)
	return buf.Bytes()
}

func decode(buf []byte, len int) (StateMoveMessage, error) {
	var decoded StateMoveMessage
	err := gob.NewDecoder(bytes.NewBuffer(buf[0:len])).Decode(&decoded)
	if err != nil {
		return StateMoveMessage{}, err
	}
	return decoded, nil
}

func ReadConfig(filepath string) *ClientConfig {
	configFile := filepath
	configData, err := ioutil.ReadFile(configFile)
	CheckErr(err, "reading config file")

	config := new(ClientConfig)
	err = json.Unmarshal(configData, config)
	CheckErr(err, "parsing config data")

	return config
}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
