package gol

import (
	"errors"
	"fmt"
	"log"
	"net/rpc"
	"sync"
	"uk.ac.bris.cs/gameoflife/util"
)

type Point struct {
	cell  util.Cell
	value bool
}

// eg. check from [0, 10)
type SlaveId struct {
	RowStart int // contain
	RowEnd   int // not contain
}

type SlaveConfigParam struct{}

type SlaveConfigResponse struct {
	Id     SlaveId
	Params Params
}

type CheckNextTurnParam struct {
	Id SlaveId
}

type CheckNextTurnResponse struct {
	AllReady   bool // all slave report there
	MissSlaves []SlaveId
	Exit       bool
}

type NextTurnParam struct {
	Id SlaveId
}

type NextTurnResponse struct {
	Turn  int
	Edges []Point
}

type ReportParam struct {
	Id      SlaveId
	Turn    int
	MyState []Point
}
type ReportResponse struct {
}

type GolMasterAPI interface {
	FetchMyConfig(param *SlaveConfigParam, response *SlaveConfigResponse) error
	CheckNextTurn(param *CheckNextTurnParam, response *CheckNextTurnResponse) error
	FetchNextTurn(param *NextTurnParam, response *NextTurnResponse) error
	ReportMyState(param *ReportParam, response *ReportResponse) error
}

type MasterHandle struct {
	OnSlaveFinish  func(points []Point) // set slave's points
	OnTurnComplete func(turn int)
	GetByIndex     func(cell util.Cell) Point // read by index
	CheckExit      func() bool
}

type GolMasterServer struct {
	params     Params
	slaveCount int
	handle     *MasterHandle

	slaveTurnLock sync.Mutex
	slaveTurnMap  map[SlaveId]int // register each salve turn
	thisTurn      int

	reportStateMap map[SlaveId][]Point
}

const (
	NotTake = -1
	Init    = 0
)

func NewGolMasterServer(params Params, slaveCount int, handle *MasterHandle) *GolMasterServer {
	// init slaveTurnMap
	var slaveTurnMap = make(map[SlaveId]int)
	c := params.ImageWidth / slaveCount
	for i := 0; i < slaveCount; i++ {
		s := i * c
		e := s + c
		if e > params.ImageWidth {
			e = params.ImageWidth
		}
		salveId := SlaveId{RowStart: s, RowEnd: e}
		slaveTurnMap[salveId] = NotTake
	}
	fmt.Printf("master with %#v, slave count %#v, init slaveTurnMap is %#v \n", params, slaveCount, slaveTurnMap)

	return &GolMasterServer{
		params:         params,
		slaveCount:     slaveCount,
		handle:         handle,
		slaveTurnMap:   slaveTurnMap,
		thisTurn:       Init,
		reportStateMap: make(map[SlaveId][]Point, slaveCount),
	}
}

func (g *GolMasterServer) setHandle(handle *MasterHandle) {
	g.handle = handle
}

func (g *GolMasterServer) FetchMyConfig(param *SlaveConfigParam, response *SlaveConfigResponse) error {
	g.slaveTurnLock.Lock()
	defer g.slaveTurnLock.Unlock()

	for slaveId, t := range g.slaveTurnMap {
		if t == NotTake {
			response.Params = g.params
			response.Id = slaveId
			g.slaveTurnMap[slaveId] = Init
			return nil
		}
	}
	return nil
}

func (g *GolMasterServer) CheckNextTurn(param *CheckNextTurnParam, response *CheckNextTurnResponse) error {
	response.AllReady = true
	for slaveId, t := range g.slaveTurnMap {
		if t != g.thisTurn {
			response.AllReady = false
			response.MissSlaves = append(response.MissSlaves, slaveId)
		}
	}
	response.Exit = g.handle.CheckExit()
	return nil
}

func (g *GolMasterServer) FetchNextTurn(param *NextTurnParam, response *NextTurnResponse) error {
	// send slave's edges
	if left := param.Id.RowStart - 1; left > 0 {
		for j := 0; j < g.params.ImageHeight; j++ {
			point := g.handle.GetByIndex(util.Cell{X: left, Y: j})
			response.Edges = append(response.Edges, point)
		}
	}

	if right := param.Id.RowEnd; right < g.params.ImageWidth {
		for j := 0; j < g.params.ImageHeight; j++ {
			point := g.handle.GetByIndex(util.Cell{X: right, Y: j})
			response.Edges = append(response.Edges, point)
		}
	}
	response.Turn = g.thisTurn
	return nil
}

func (g *GolMasterServer) ReportMyState(param *ReportParam, response *ReportResponse) error {
	if param.Turn != g.thisTurn {
		return errors.New("invalid turn")
	}
	// set the salve's state
	g.slaveTurnLock.Lock()
	defer g.slaveTurnLock.Unlock()
	g.reportStateMap[param.Id] = param.MyState
	if len(g.reportStateMap) == len(g.slaveTurnMap) && len(g.reportStateMap) == g.slaveCount {
		// report to handler
		for _, state := range g.reportStateMap {
			g.handle.OnSlaveFinish(state)
		}
		g.handle.OnTurnComplete(g.thisTurn)
		g.thisTurn += 1
		g.reportStateMap = make(map[SlaveId][]Point, g.slaveCount)
	}
	return nil
}

type GolSlaveClient struct {
	client *rpc.Client
}

func NewGolSlaveClient(ip string, port int) *GolSlaveClient {
	client, err := rpc.DialHTTP("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		log.Fatal("dialing:", err)
	}
	return &GolSlaveClient{
		client: client,
	}
}

func (gc *GolSlaveClient) FetchMyConfig() *SlaveConfigResponse {
	var param = &SlaveConfigParam{}
	var response = &SlaveConfigResponse{}
	err := gc.client.Call("GolMasterServer.FetchMyConfig", param, response)
	if err != nil {
		log.Fatal("client fetch error:", err)
	}
	return response
}

func (gc *GolSlaveClient) CheckNextTurn(param *CheckNextTurnParam) *CheckNextTurnResponse {
	var response = &CheckNextTurnResponse{}
	err := gc.client.Call("GolMasterServer.CheckNextTurn", param, response)
	if err != nil {
		log.Fatal("client fetch error:", err)
	}
	return response
}

func (gc *GolSlaveClient) FetchNextTurn(param *NextTurnParam) *NextTurnResponse {
	var response = &NextTurnResponse{}
	err := gc.client.Call("GolMasterServer.FetchNextTurn", param, response)
	if err != nil {
		log.Fatal("client fetch error:", err)
	}
	return response
}

func (gc *GolSlaveClient) ReportMyState(param *ReportParam) {
	var response = &ReportResponse{}
	err := gc.client.Call("GolMasterServer.ReportMyState", param, response)
	if err != nil {
		log.Fatal("client fetch error:", err)
	}
	return
}

type MSCtrl struct {
	Server *GolMasterServer
	Client *GolSlaveClient
}
