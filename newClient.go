package main

import (
	"bytes"
	"errors"

	"encoding/gob"
	"encoding/json"
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
	NimServerAddress     string // Maximum 8 nim servers will be provided
	TracingServerAddress string
	Secret               []byte
	TracingIdentity      string
	// FCheck stuff:
	//FCheckAckLocalAddr   string
	//FCheckHbeatLocalAddr string
	//FCheckLostMsgsThresh uint8
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

/** New tracing structs introduced in A2 **/

//type NewNimServer struct {
//	NimServerAddress string
//}
//
//type NimServerFailed struct {
//	NimServerAddress string
//}
//
//type AllNimServersDown struct {
//}

/** Message structs **/

type StateMoveMessage struct {
	GameState []uint8
	MoveRow   int8
	MoveCount int8
	//TracingServerAddr string               // ADDED IN A2
	//Token             tracing.TracingToken // ADDED IN A2
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: client [seed]")
		return
	}
	arg, err := strconv.Atoi(os.Args[1])
	CheckErr(err, "Provided seed could not be converted to integer", arg)
	seed := int8(arg)

	config := ReadConfig("./config/client_config.json")

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

	bufout := make([]byte, 1024)
	bufin := make([]byte, 1024)

	raddr, err := net.ResolveUDPAddr("udp", config.NimServerAddress)
	CheckErr(err, "Error in resolving remote address", raddr)
	laddr, err := net.ResolveUDPAddr("udp", config.ClientAddress)
	CheckErr(err, "Error in resolving local address", laddr)

	conn, err := net.DialUDP("udp", laddr, raddr)
	CheckErr(err, "Error in connecting to server", conn)
	defer conn.Close()

	trace.RecordAction(ClientMove{nil, -1, seed})
	bufout, err = Marshal(ClientMove{nil, -1, seed})
	_, err = conn.Write(bufout)
	CheckErr(err, "Error in writing to the server")

	for {

		n, _, err := conn.ReadFromUDP(bufin)
		CheckErr(err, "Error in reading from server")

		ServerMove := StateMoveMessage{}
		err = Unmarshal(bufin[:n], &ServerMove)
		CheckErr(err, "Error in marshalling the server response")
		trace.RecordAction(ServerMoveReceive(ServerMove))

		if ServerMove.GameState == nil && ServerMove.MoveRow == -1 {
			bufout, err = Marshal(ClientMove{nil, -1, seed})
			trace.RecordAction(ClientMove{nil, -1, seed})
			_, err = conn.Write(bufout)
			CheckErr(err, "Error in writing to the server")
		} else if ServerMove.GameState != nil && ServerMove.MoveCount > 0 {
			if allzeros(ServerMove.GameState) {
				trace.RecordAction(GameComplete{Winner: "Server"})
				break
			}
			newMove := play(ServerMove.GameState)
			bufout, err = Marshal(newMove)
			CheckErr(err, "Error in marshaling server newMove", bufout)
			trace.RecordAction(ClientMove(newMove))
			_, err = conn.Write(bufout)
			CheckErr(err, "Error in marshaling the new move")
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

func Marshal(v interface{}) ([]byte, error) {
	var network bytes.Buffer
	enc := gob.NewEncoder(&network)
	err := enc.Encode(v)
	CheckErr(err, "Error in encoding the server data")
	return network.Bytes(), err

}

func Unmarshal(b []byte, move interface{}) error {
	network := bytes.NewBuffer(b)
	dec := gob.NewDecoder(network)
	err := dec.Decode(move)
	CheckErr(err, "Error in Decoding the server data")
	return err

}

func play(move []uint8) StateMoveMessage {
	netxMove, err := normal(move)
	if err != nil {
		fmt.Println(err)
	}
	return *netxMove
}

func normal(board []uint8) (*StateMoveMessage, error) {
	for i := 0; i < len(board); i++ {
		if board[i] > 0 {
			board[i] -= 1
			return &StateMoveMessage{GameState: board, MoveRow: int8(i), MoveCount: 1}, nil

		}
	}
	return nil, errors.New("no move to make")
}

func allzeros(arr []uint8) bool {
	count := 0
	for i := 0; i < len(arr); i++ {
		if arr[i] == 0 {
			count++
		}
	}
	if count == len(arr) {
		return true
	} else {
		return false
	}

}
