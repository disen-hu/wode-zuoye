package main

import (
	"flag"
	"fmt"
	"log"
	"net/rpc"
	"time"
	"uk.ac.bris.cs/gameoflife/cs"
	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/sdl"
)

func RunClient(params gol.Params, ip string, port int) {
	keyPresses := make(chan rune, 10)
	events := make(chan gol.Event, 1000)

	client, err := rpc.DialHTTP("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		log.Fatal("dialing:", err)
	}
	// send the Key
	go func() {
		for key := range keyPresses {
			param := &cs.KeyPressParam{Key: key}
			response := &cs.KeyPressResponse{}
			err := client.Call("GolServer.KeyPress", param, response)
			if err != nil {
				log.Fatal("send event error:", err)
			}

			// Exit
			if key == 'q' {
				fmt.Println("Exit Client")
				time.Sleep(1)
				close(events)
			}
		}
	}()

	sdl.Start(params, events, keyPresses)
}

func main() {
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

	fmt.Println("IP:", ip)
	fmt.Println("Port:", port)

	// fake params
	var params = gol.Params{
		Turns:       1,
		Threads:     1,
		ImageWidth:  512,
		ImageHeight: 512,
	}

	RunClient(params, ip, port)
}
