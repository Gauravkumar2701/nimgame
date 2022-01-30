package main

import (
	"math/rand"
	"testing"
)

func genEmptyBoards(n int) [][]uint8 {
	var boards [][]uint8
	for i := 0; i < n; i++ {
		rows := rand.Intn(14) + 3
		b := make([]uint8, rows)
		for i := 0; i < rows; i++ {
			b[i] = uint8(0)
		}
		boards = append(boards, b)
	}
	return boards
}

func genBoards(n int) [][]uint8 {
	var boards [][]uint8
	for i := 0; i < n; i++ {
		b := GenerateBoard(int64(i))
		boards = append(boards, b)
	}
	return boards
}

func TestEmptyBoard(t *testing.T) {
	// empty boards should all be empty
	emptyBoards := genEmptyBoards(15)
	t.Logf("Boards: %v\n", emptyBoards)
	for _, b := range emptyBoards {
		isEmpty := emptyBoard(b)
		if !isEmpty {
			t.Errorf("board should be empty: %v\n", b)
		}
	}

	// non-empty boards should all be non-empty
	nonEmptyBoards := genBoards(15)
	t.Logf("Boards: %v\n", nonEmptyBoards)
	for _, b := range nonEmptyBoards {
		isEmpty := emptyBoard(b)
		if isEmpty {
			t.Errorf("board should not be empty: %v\n", b)
		}
	}
}

func TestNormalMove(t *testing.T) {
	// a normal move is to take one from the first non-zero row
	boards := genBoards(15)
	for _, b := range boards {
		t.Logf("Board: %v\n", b)
		// record the first element before move
		prev0 := b[0]
		st, err := normalMove(b)
		t.Logf("after move: %v\n", st.GameState)
		// All boards are non-empty, so should not error
		if err != nil {
			t.Errorf("a normal move should be made on board: %v\n", b)
		}
		// the board after move
		b2 := st.GameState
		// since the board in non-empty in all rows, we should always remove 1 item from row 0
		if (prev0-b2[0]) != 1 || st.MoveRow != 0 || st.MoveCount != 1 {
			t.Errorf("made a wrong move: %v\n", st)
		}
	}

	board := []uint8{1, 9, 1, 5}
	t.Logf("Board: %v\n", board)
	st, _ := normalMove(board)
	t.Logf("after move: %v\n", st)
	if st.GameState[0] != 0 || st.MoveRow != 0 || st.MoveCount != 1 {
		t.Errorf("made a wrong move: %v\n", st)
	}
}

func TestBoardGen(t *testing.T) {
	boards := genBoards(15)
	for _, b := range boards {
		sum := nimSum(b)
		if sum == 0 {
			t.Errorf("board nim sum should be non-zero: %v\n", b)
		}
	}
}

func TestBestMove(t *testing.T) {
	boards := genBoards(15)
	for _, b := range boards {
		t.Logf("Board: %v\n", b)
		st := bestMove(b)
		t.Logf("after move: %v\n", st.GameState)
		sum := nimSum(st.GameState)
		// the generated Boards are guaranteed to have non-zero nim sum
		// therefore it's always possible to make nim-sum zero
		if sum != 0 {
			t.Errorf("nim sum should be zero after best move: %v\n", st)
		}
	}
}
