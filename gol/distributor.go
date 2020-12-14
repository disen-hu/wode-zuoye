package gol

import (
	"fmt"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	filename   chan<- string
	output     chan<- uint8
	input      <-chan uint8
	keyPresses <-chan rune
	hc         *MSCtrl
}

// cycle
func getNeighbours(x, y, maxWidth, maxHeight, width, height int) []util.Cell {
	if x == 0 || x == maxWidth || y == 0 || y == maxHeight {
		return []util.Cell{
			{(x - 1 + width) % width, (y - 1 + height) % height},
			{(x - 1 + width) % width, y},
			{(x - 1 + width) % width, (y + 1) % height},
			{x, (y - 1 + height) % height},
			{x, (y + 1) % height},
			{(x + 1) % width, (y - 1 + height) % height},
			{(x + 1) % width, y},
			{(x + 1) % width, (y + 1) % height},
		}
	}
	return []util.Cell{
		{x - 1, y - 1},
		{x - 1, y},
		{x - 1, y + 1},
		{x, y - 1},
		{x, y + 1},
		{x + 1, y - 1},
		{x + 1, y},
		{x + 1, y + 1},
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	// Create a 2D slice to store the world.
	panel := make([][]bool, p.ImageWidth)
	for i := range panel {
		panel[i] = make([]bool, p.ImageHeight)
	}

	// load init cells
	c.ioCommand <- ioInput
	c.filename <- fmt.Sprintf("%vx%v", p.ImageWidth, p.ImageHeight)

	var initCells = make([]util.Cell, 0)
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			val := <-c.input
			if val == 255 {
				panel[x][y] = true
				initCells = append(initCells, util.Cell{X: x, Y: y})
			}
		}
	}

	// For all initially alive cells send a CellFlipped Event.
	for _, cell := range initCells {
		c.events <- CellFlipped{CompletedTurns: 0, Cell: cell}
	}
	c.events <- TurnComplete{CompletedTurns: 0}
	// Execute all turns of the Game of Life.

	maxWidth := p.ImageWidth - 1
	maxHeight := p.ImageHeight - 1

	// check One Cell live or die.
	var checkOneCell = func(x, y int) bool {
		var aliveCount int
		neighbours := getNeighbours(x, y, maxWidth, maxHeight, p.ImageWidth, p.ImageHeight)
		for _, cell := range neighbours {
			if panel[cell.X][cell.Y] {
				aliveCount++
			}
		}

		// 1. any live cell with fewer than two live neighbours dies
		if panel[x][y] && aliveCount < 2 {
			return false
		}
		// 2. any live cell with two or three live neighbours is unaffected
		if panel[x][y] && aliveCount >= 2 && aliveCount <= 3 {
			return panel[x][y]
		}
		// 3. any live cell with more than three live neighbours dies
		if panel[x][y] && aliveCount > 3 {
			return false
		}
		// 4. any dead cell with exactly three live neighbours becomes alive
		if !panel[x][y] && aliveCount == 3 {
			return true
		}
		return false
	}

	// read alive cells
	var getAliveCells = func() []util.Cell {
		var alive []util.Cell
		for i, rows := range panel {
			for j, v := range rows {
				if v {
					alive = append(alive, util.Cell{X: i, Y: j})
				}
			}
		}
		return alive
	}

	var writePanel = func(t int) {
		// write image
		c.ioCommand <- ioOutput
		c.filename <- fmt.Sprintf("%vx%vx%v", p.ImageWidth, p.ImageHeight, t)
		for y := 0; y < p.ImageHeight; y++ {
			for x := 0; x < p.ImageWidth; x++ {
				if panel[x][y] {
					c.output <- 255
				} else {
					c.output <- 0
				}
			}
		}
	}

	turn := 0
	runExit := false
	pause := false
	// report AliveCellsCount
	if c.hc == nil || p.IsMaster {
		go func() {
			for range time.Tick(2 * time.Second) {
				if !runExit {
					c.events <- AliveCellsCount{CompletedTurns: turn, CellsCount: len(getAliveCells())}
				}
			}
		}()
	}

	// ms model
	if c.hc != nil && p.IsMaster {
		handle := &MasterHandle{}
		handle.OnTurnComplete = func(t int) {
			turn++
			c.events <- TurnComplete{CompletedTurns: turn}
		}
		handle.OnSlaveFinish = func(points []Point) {
			for _, p := range points {
				panel[p.cell.X][p.cell.Y] = p.value
			}
		}
		handle.CheckExit = func() bool {
			return runExit
		}
		handle.GetByIndex = func(cell util.Cell) Point {
			return Point{cell: cell, value: panel[cell.X][cell.Y]}
		}
		c.hc.Server.setHandle(handle)
	}

	// master or single
	if c.hc == nil || p.IsMaster {
		// see keyPresses
		go func() {
			for {
				if runExit {
					break
				}
				ctl := <-c.keyPresses
				switch ctl {
				case 's':
					writePanel(turn)
					fmt.Println("Save Success")
				case 'q':
					writePanel(turn)
					runExit = true
					fmt.Println("Exit")
				case 'p':
					pause = !pause
					if pause {
						fmt.Println("Current running turn is ", turn)
					} else {
						fmt.Println("Continuing")
					}
				}
			}
		}()
	}

	// single mode
	if c.hc == nil {
		for turn < p.Turns && !runExit {
			if pause {
				continue
			}
			// DEBUG show the running state
			/*
				alive := getAliveCells()
				s := util.AliveCellsToString(alive, alive, p.ImageWidth, p.ImageHeight)
				fmt.Println("check on turn ", turn)
				fmt.Println(s)
			*/

			// 1. check all alive cells && there neighbour
			var checkRow = make(chan int)
			var newDieCells = make(chan util.Cell, 10000)
			var newLiveCells = make(chan util.Cell, 10000)

			go func() {
				for i := 0; i < p.ImageWidth; i++ {
					checkRow <- i
				}
				close(checkRow)
			}()

			var wg sync.WaitGroup
			for i := 0; i < p.Threads; i++ {
				wg.Add(1)
				go func() {
					for index := range checkRow {
						for j := 0; j < p.ImageHeight; j++ {
							oldLive := panel[index][j]
							live := checkOneCell(index, j)
							if oldLive && !live {
								newDieCells <- util.Cell{X: index, Y: j}
							}
							if !oldLive && live {
								newLiveCells <- util.Cell{X: index, Y: j}
							}
						}
					}
					wg.Done()
				}()
			}

			wg.Wait()
			close(newDieCells)
			close(newLiveCells)
			turn++
			// wait result
			for cell := range newDieCells {
				panel[cell.X][cell.Y] = false
				c.events <- CellFlipped{CompletedTurns: turn, Cell: cell}
			}
			for cell := range newLiveCells {
				panel[cell.X][cell.Y] = true
				c.events <- CellFlipped{CompletedTurns: turn, Cell: cell}
			}

			c.events <- TurnComplete{CompletedTurns: turn}
		}
		// not quit
		if !runExit {
			// write image
			writePanel(p.Turns)
		}
		// send FinalTurnComplete
		alive := getAliveCells()
		c.events <- FinalTurnComplete{CompletedTurns: turn, Alive: alive}
		runExit = true
	}

	// ms model
	if c.hc	!= nil{
		if p.IsMaster {
			for {
				time.Sleep(1)
			}
		} else {
			var myTurn = 0
			config := c.hc.Client.FetchMyConfig()
			for {
				cnp := &CheckNextTurnParam{Id: config.Id}
				cnr := c.hc.Client.CheckNextTurn(cnp)
				if cnr.Exit{
					break
				}
				if !cnr.AllReady{
					continue
				}
				// calc my turn
				np := &NextTurnParam{Id: config.Id}
				nr := c.hc.Client.FetchNextTurn(np)
				myTurn = nr.Turn
				// load the edge
				for _, p := range nr.Edges{
					panel[p.cell.X][p.cell.Y] = p.value
				}
				// check my panel
				var newDieCells = make([]util.Cell, 0, 1000)
				var newLiveCells = make([]util.Cell, 0, 1000)

				for i:= config.Id.RowStart; i < config.Id.RowEnd; i++{
					for j := 0; j < p.ImageHeight; j++ {
						oldLive := panel[i][j]
						live := checkOneCell(i, j)
						if oldLive && !live {
							newDieCells = append(newDieCells, util.Cell{X: i, Y: j})

						}
						if !oldLive && live {
							newLiveCells = append(newLiveCells, util.Cell{X: i, Y: j})
						}
					}
				}

				for _, cell := range newDieCells {
					panel[cell.X][cell.Y] = false
					c.events <- CellFlipped{CompletedTurns: turn, Cell: cell}
				}
				for _, cell := range newLiveCells {
					panel[cell.X][cell.Y] = true
					c.events <- CellFlipped{CompletedTurns: turn, Cell: cell}
				}

				// report my state
				rp := &ReportParam{
					Id:      config.Id,
					Turn:    myTurn,
				}
				for i:= config.Id.RowStart; i < config.Id.RowEnd; i++{
					for j := 0; j < p.ImageHeight; j++ {
						rp.MyState = append(rp.MyState, Point{
							cell:  util.Cell{X: i, Y: j},
							value: panel[i][j],
						})
					}
				}
				c.hc.Client.ReportMyState(rp)
			}
		}

	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
