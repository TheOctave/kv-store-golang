package main

import (
	"log"
	"time"
)

func main() {
	c1 := make(chan string, 1)
	c2 := make(chan string, 1)

	go func() {
		c1 <- "one"
		c2 <- "two"
	}()

	for {
		select {
		case msg1 := <-c1:
			log.Printf("received %q", msg1)
		case msg2 := <-c2:
			log.Printf("received %q", msg2)
		default:
			log.Printf(".")
			time.Sleep(time.Second)
		}
	}
}
