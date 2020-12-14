package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"runtime"
	"time"

	"uk.ac.bris.cs/gameoflife/cs"
	"uk.ac.bris.cs/gameoflife/gol"
)

func RunServer(params gol.Params, ip string, port int) {
	keyPresses := make(chan rune, 10)
	events := make(chan gol.Event, 1000)
	var OnKeyPress = func(c rune) {
		keyPresses <- c
	}
	var server = &cs.GolServer{OnKeyPress: OnKeyPress}

	rpc.Register(server)
	rpc.HandleHTTP()
	l, e := net.Listen("tcp", fmt.Sprintf("%s:%d", ip, port))
	if e != nil {
		log.Fatalln("listen error:", e)
	}
	go http.Serve(l, nil)
	// drop events
	go func() {
		for {
			time.Sleep(1)
			_, ok := <-events
			if !ok {
				fmt.Println("running done")
				os.Exit(0)
			}
		}
	}()
	// start
	gol.Run(params, events, keyPresses, nil)
}

func main() {
	runtime.LockOSThread()
	var params gol.Params

	flag.IntVar(
		&params.Threads,
		"t",
		8,
		"Specify the number of worker threads to use. Defaults to 8.")

	flag.IntVar(
		&params.ImageWidth,
		"w",
		512,
		"Specify the width of the image. Defaults to 512.")

	flag.IntVar(
		&params.ImageHeight,
		"h",
		512,
		"Specify the height of the image. Defaults to 512.")

	flag.IntVar(
		&params.Turns,
		"turns",
		10000000000,
		"Specify the number of turns to process. Defaults to 10000000000.")

	var ip string
	var port int
	flag.StringVar(
		&ip,
		"ip",
		"3.93.6.135",
		"Specify the ip address. Defaults to my ec2 ip.")

	flag.IntVar(
		&port,
		"p",
		7890,
		"Specify the port number. Defaults to 7890.")

	flag.Parse()

	fmt.Println("Threads:", params.Threads)
	fmt.Println("Width:", params.ImageWidth)
	fmt.Println("Height:", params.ImageHeight)
	fmt.Println("IP:", ip)
	fmt.Println("Port:", port)

	RunServer(params, ip, port)

	for {
		time.Sleep(1)
	}
}
