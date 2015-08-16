package main

import (
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"os"
)

//Central handler responsible for the pubsub mechanism, adding and removing clients
type CentralProcess struct {
	//A map for easy access of client handlers. The boolean isn't really used
	clientHandlers map[*ClientHandlerProcess]bool
	//A channel where incoming messages are published to all clients
	publish chan string
	//A channel where ClientHandlers can add themselves for publishing
	add chan *ClientHandlerProcess
	//A channel where ClientHandlers can remove themselves for publishing
	remove chan *ClientHandlerProcess
}

//Creates a new central process
func NewCentralProcess() *CentralProcess {
	return &CentralProcess{
		make(map[*ClientHandlerProcess]bool),
		make(chan string),
		make(chan *ClientHandlerProcess),
		make(chan *ClientHandlerProcess),
	}
}

//Starts listening for messages on the central process. Run this as a separate goroutine
func (centralProcess *CentralProcess) Start() {
	for {
		select {
		case clientHandler := <-centralProcess.add:
			centralProcess.clientHandlers[clientHandler] = false

		case clientHandler := <-centralProcess.remove:
			delete(centralProcess.clientHandlers, clientHandler)

		case message := <-centralProcess.publish:
			for clientHandler := range centralProcess.clientHandlers {
				clientHandler.sendMsgChan <- message
			}
		}
	}
}

//Client handling process
type ClientHandlerProcess struct {
	centralProcess *CentralProcess
	sendMsgChan    chan string
}

func (clientHandler ClientHandlerProcess) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println("new cli from", r.RemoteAddr)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	websock, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err.Error())
		return
	}

	//Send a message to add this ClientHandler to the pubsub system
	clientHandler.centralProcess.add <- &clientHandler

	//Deferred function to clean up the socket and remove the ClientHandler from the pubsub map
	cleanUp := func() {
		websock.Close()
		clientHandler.centralProcess.remove <- &clientHandler
	}

	//Goroutine to send a receive a message from the central process and send it on to the websocket
	go func() {
		defer cleanUp()
		for {
			msg := <-clientHandler.sendMsgChan
			err := websock.WriteMessage(websocket.TextMessage, []byte(msg))
			if err != nil {
				log.Println(err.Error())
				break
			}
		}
	}()

	//Goroutine to receieve a message from a websocket and send it to the central process for publishing to all
	go func() {
		defer cleanUp()
		for {
			_, bytes, err := websock.ReadMessage()
			msg := r.RemoteAddr + ": " + string(bytes)
			if err != nil {
				log.Println(err.Error())
				break
			} else {
				clientHandler.centralProcess.publish <- msg
			}
		}
	}()
}

func main() {
	centralProcess := NewCentralProcess()
	go centralProcess.Start()

	dir, err := os.Getwd()
	if err != nil {
		log.Println(err.Error())
		return
	}

	addr := "0.0.0.0:4000"

	//Handler to serve the initial html containing the frontend chat
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, dir+"/index.html")
	})

	//Handles the websocket connectons
	http.Handle("/sock", ClientHandlerProcess{centralProcess, make(chan string)})

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Println(err.Error())
	}
}
