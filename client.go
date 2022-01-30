package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DistributedClocks/tracing"
	"io/ioutil"
	"net"
	"os"
	"strconv"
)

/** Config struct **/

type ClientConfig struct {
	ClientAddress        string
	NimServerAddress     string
	TracingServerAddress string
	Secret               []byte
	TracingIdentity      string
}

/** Tracing structs **/

type GameStart struct {
	Seed int8
}

type ClientMove StateMoveMessage

type ServerMoveReceive StateMoveMessage

type GameComplete struct {
	Winner string
}

/** Message structs **/

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

	config := ReadConfig("../config/client_config.json")
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

	buf := make([]byte, 5000)
	bufOut := make([]byte, 5000)

	remoteadrr, err := net.ResolveUDPAddr("udp", config.NimServerAddress)
	CheckErr(err, "Error in resolving server address", remoteadrr)

	laddr, err := net.ResolveUDPAddr("udp", config.ClientAddress)
	CheckErr(err, "Error in resolving local addr", laddr)

	conn, err := net.DialUDP("udp", laddr, remoteadrr)
	CheckErr(err, "Error in connecting to server", conn)

	defer conn.Close()

	bufOut, err = Marshal(ClientMove{nil, -1, seed})
	CheckErr(err, "Error in marshalling the server message", bufOut)

	trace.RecordAction(ClientMove{nil, -1, seed})

	_, err = conn.Write(bufOut)
	CheckErr(err, "Error in sending message to server")

	for {

		// Reading message send from the server
		n, _, err := conn.ReadFromUDP(buf)
		CheckErr(err, "Error in reading from bufIn")

		ServerMove := StateMoveMessage{}
		err = Unmarshal(buf[:n], &ServerMove)
		CheckErr(err, "Error in Unmarshalling the server message")
		trace.RecordAction(ServerMoveReceive(ServerMove))

		// Sending message to server on when server start their first move
		if ServerMove.GameState == nil && ServerMove.MoveRow == -1 {
			bufOut, err = Marshal(ClientMove{nil, -1, seed})
			CheckErr(err, "Error in marshalling the message", bufOut)

			_, err = conn.Write(bufOut)
			CheckErr(err, "Error is sending message to server")

			trace.RecordAction(ClientMove{nil, -1, seed})

		} else if ServerMove.GameState != nil && ServerMove.MoveCount > 0 {

			state := nimsum(ServerMove.GameState)
			if state {
				trace.RecordAction(GameComplete{Winner: "Server"})
				break
			}

			newMove := play(ServerMove)

			trace.RecordAction(ClientMove(newMove))

			bufOut, err := Marshal(newMove)

			_, err = conn.Write(bufOut)

			CheckErr(err, "Error in sending message to server")

		}

	}

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

func Unmarshal(input []byte, move interface{}) error {
	network := bytes.NewBuffer(input)
	dec := gob.NewDecoder(network)
	err := dec.Decode(move)
	return err
}
func Marshal(move interface{}) ([]byte, error) {
	var network bytes.Buffer
	enc := gob.NewEncoder(&network)
	err := enc.Encode(move)
	return network.Bytes(), err
}

func play(move StateMoveMessage) StateMoveMessage {

	nextMove, err := normalmove(move.GameState)
	if err != nil {
		fmt.Println(err)
	}

	return *nextMove

}

func normalmove(board []uint8) (*StateMoveMessage, error) {
	for i := 0; i < len(board); i++ {
		if board[i] > 0 {
			board[i] -= 1
			return &StateMoveMessage{
				board,
				int8(i),
				1,
			}, nil
		}
	}
	return nil, errors.New("no move to make")
}

func nimsum(move []uint8) bool {
	state := false
	count := 0
	for i := 0; i < len(move); i++ {
		if move[i] == 0 {
			count++
		}
	}
	if count == len(move) {
		state = true
	}
	return state
}
