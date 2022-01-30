package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"

	"github.com/DistributedClocks/tracing"
)

/** Config struct **/

type ServerConfig struct {
	NimServerAddress     string
	TracingServerAddress string
	Secret               []byte
	TracingIdentity      string
}

/** Tracing structs **/

type ClientMoveReceive StateMoveMessage

type ServerMove StateMoveMessage

/** Message structs **/

type StateMoveMessage struct {
	GameState []uint8
	MoveRow   int8
	MoveCount int8
}

type NetworkConditioner func()

type UDPConditioners struct {
	DuplicateConditioner NetworkConditioner
	DelayConditioner     NetworkConditioner
	LossConditioner      NetworkConditioner
}

type UDPConnection struct {
	Conds *UDPConditioners
	Conn  *net.UDPConn
	BufIn []byte
}

func (udp *UDPConnection) Close() {
	udp.Conn.Close()
}

func (udp *UDPConnection) ReadFrom() (n int, raddr *net.UDPAddr, err error) {
	n, raddr, err = udp.Conn.ReadFromUDP(udp.BufIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error receiving connection: %v\n", err)
	}
	return
}

func (udp *UDPConnection) WriteTo(packet []byte, raddr *net.UDPAddr) {
	_, err := udp.Conn.WriteToUDP(packet, raddr)
	if err != nil {
		fmt.Printf("Error sending UDP packet to remote address: %v\n", raddr)
	}
}

func UDPAdapter(conn *net.UDPConn, bufsize int) *UDPConnection {
	buf := make([]byte, bufsize)
	return &UDPConnection{nil, conn, buf}
}

func main() {
	// init server configs
	config := readServerConfig("../config/server_config.json")

	// start tracing
	tracer := initTracer(config)
	defer tracer.Close()
	trace := tracer.CreateTrace()

	// start udp listening
	udp := startListenUDP(config)
	defer udp.Close()

	// have a data structure tracking last known game states/SMMs
	clientGames := make(map[string]StateMoveMessage) // raddr: last known state
	clientDifficulties := make(map[string]int8)

	for {
		// remember to have a timeout on this
		n, raddr, err := udp.ReadFrom()
		if err != nil {
			continue
		}

		raddrStr := raddr.String()
		fmt.Printf("Remote address %v", raddrStr)
		clientMove := StateMoveMessage{}
		err = Unmarshal(udp.BufIn[:n], &clientMove)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error unmarshalling message from connection: %v\n", err)
			continue
		}
		trace.RecordAction(ClientMoveReceive(clientMove))

		// check if there's an ongoing game for the sender
		lastMove, exists := clientGames[raddrStr]
		var servMove StateMoveMessage
		// GameStart message
		if clientMove.GameState == nil && clientMove.MoveRow == -1 {
			// new game
			seed := clientMove.MoveCount
			newGameState := GenerateBoard(int64(seed))
			servMove = StateMoveMessage{
				GameState: newGameState,
				MoveRow:   -1,
				MoveCount: seed,
			}
			clientDifficulties[raddrStr] = seed & 1
		} else if !exists {
			// not a GameStart message and no ongoing games
			// ignore the ill-formed message
			continue
		} else {
			ver := CheckMove(clientMove, lastMove)
			if !ver {
				servMove = lastMove
			} else {
				servMove = Play(clientMove, clientDifficulties[raddrStr])
			}
		}

		// save the game
		clientGames[raddrStr] = servMove
		trace.RecordAction(ServerMove(servMove))

		var bufOut []byte
		bufOut, err = Marshal(servMove)
		CheckErr(err, "Server move failed to marshal")

		// At this point buf contains a reply that we send back to the raddr.
		udp.WriteTo(bufOut, raddr)
	}
}

// func serverLoop(conn *UDPConnection) {}

// Given a board game state, calculate a next move to return
func Play(move StateMoveMessage, mode int8) StateMoveMessage {
	board := move.GameState

	// all rows empty, should not happen
	// should this value be encountered, it is to be considered an admission of defeat -- not required to show
	if emptyBoard(board) {
		return StateMoveMessage{
			GameState: nil,
			MoveRow:   -2,
			MoveCount: -2,
		}
	}

	if mode == 1 {
		// advanced strategy:
		// calculate the nimsum, and make it equal 0
		// if nimsum is already 0, make a normal move
		return bestMove(board)
	}

	// basic strategy: find the first non-empty row, and take one piece from it.
	nextMove, err := normalMove(board)
	if err != nil {
		fmt.Println(err)
	}

	return *nextMove
}

// check if the board is empty
func emptyBoard(board []uint8) bool {
	isEmpty := true
	for _, v := range board {
		if v != 0 {
			isEmpty = false
			break
		}
	}
	return isEmpty
}

// calculate the nimsum of a board
func nimSum(board []uint8) uint8 {
	sum := uint8(0)
	for _, v := range board {
		sum ^= v
	}
	return sum
}

// naive gameplay
func normalMove(board []uint8) (*StateMoveMessage, error) {
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

// advanced gameplay
// always try to make the nimsum be zero
func bestMove(board []uint8) StateMoveMessage {
	sum := nimSum(board)
	if sum != 0 {
		for i, v := range board {
			tmp := sum ^ v
			if tmp <= v {
				board[i] = tmp
				return StateMoveMessage{
					board,
					int8(i),
					int8(v - tmp),
				}
			}
		}
	}
	move, err := normalMove(board)
	CheckErr(err, "Error making a normal move: %v\n", err)
	return *move
}

// lastmove is the last move server sent to a client
// incmove is the normal move received for that client
// check that this move is valid, and return whether it is
func CheckMove(incmove StateMoveMessage, lastmove StateMoveMessage) bool {
	lastboard := lastmove.GameState
	incboard := incmove.GameState

	// Sanity checks
	// 1. borad length should not change
	// 2. MoveRow should be valid (0 <= MoveRow < len(board))
	if len(lastboard) != len(incboard) ||
		incmove.MoveRow < 0 ||
		int(incmove.MoveRow) >= len(incboard) {
		return false
	}
	// Check the validity of the move
	// 1. row counts should not change for rows not moved
	// 2. the row count for the moved row should be correctly updated
	for i := 0; i < len(incboard); i++ {
		if incboard[i] == lastboard[i] {
			continue
		} else if i == int(incmove.MoveRow) &&
			incmove.MoveCount > 0 &&
			incmove.MoveCount <= int8(lastboard[i]) &&
			incboard[i] == lastboard[i]-uint8(incmove.MoveCount) {
			continue
		}
		return false
	}

	return true
}

// generate a gameboard based on the given seed
func GenerateBoard(seed int64) []uint8 {
	// generate game borad based on the given seed
	rand.Seed(seed)
	numRows := rand.Intn(14) + 3
	board := make([]uint8, numRows)
	for i := 0; i < numRows; i++ {
		numCoins := rand.Intn(10) + 1
		board[i] = uint8(numCoins)
	}

	nimSum := nimSum(board)
	// make sure board is winnable for client
	if nimSum == 0 {
		if board[numRows-1] < 10 {
			board[numRows-1]++
		} else {
			board[numRows-1]--
		}
	}
	return board
}

func readServerConfig(path string) *ServerConfig {
	// read default server config
	configData, err := ioutil.ReadFile(path)
	CheckErr(err, "reading config file")
	config := new(ServerConfig)
	err = json.Unmarshal(configData, config)
	CheckErr(err, "parsing config data")

	// command-line args has higher priority
	if len(os.Args) == 2 {
		config.NimServerAddress = "0.0.0.0:" + os.Args[1]
	} else if len(os.Args) == 3 {
		config.NimServerAddress = os.Args[1] + ":" + os.Args[2]
	}
	return config
}

func initTracer(config *ServerConfig) *tracing.Tracer {
	return tracing.NewTracer(tracing.TracerConfig{
		ServerAddress:  config.TracingServerAddress,
		TracerIdentity: config.TracingIdentity,
		Secret:         config.Secret,
	})
}

func startListenUDP(config *ServerConfig) *UDPConnection {
	// start listening for UDP connection
	addr, err := net.ResolveUDPAddr("udp", config.NimServerAddress)
	CheckErr(err, "Error resolving UDP address: %v\n", err)
	conn, err := net.ListenUDP("udp", addr)
	CheckErr(err, "Error listening on UDP address: %v\n", err)
	return UDPAdapter(conn, 1024)
}

// Gets the byte array representation of a move, so it can be put onto the wire.
func Marshal(move interface{}) ([]byte, error) {
	var network bytes.Buffer
	enc := gob.NewEncoder(&network)
	err := enc.Encode(move)
	return network.Bytes(), err
}

func Unmarshal(input []byte, move interface{}) error {
	network := bytes.NewBuffer(input)
	dec := gob.NewDecoder(network)
	err := dec.Decode(move)
	return err
}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
