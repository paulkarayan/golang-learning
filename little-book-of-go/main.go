package main

import (
	"fmt"
	"math/rand"
	"time"
)

type Worker struct {
	id int
}

func main() {
	c := make(chan int)
	for i := 0; i < 5; i++ {
		worker := &Worker{id: i}
		go worker.process(c)
	}
	for {
		c <- rand.Int()
		time.Sleep(time.Millisecond * 10)
	}
}

func (w *Worker) process(c chan int) {
	for {
		data := <-c
		fmt.Printf("worker %d got %d", w.id, data)
	}
}
