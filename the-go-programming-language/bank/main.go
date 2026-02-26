// ch 9 - concurrency w shared vars

package main

import (
	"fmt"
	"sync"
)

var deposits = make(chan int)
var balances = make(chan int)

func Deposit(amount int) { deposits <- amount }

// blocks until value recieved, then returns it
func Balance() int { return <-balances }

func printState() {
	fmt.Println("balance:", Balance())
}

func teller() {
	var balance int // confined to teller goroutine, so solves concurrent access problem
	for {
		select {
		case amount := <-deposits:
			balance += amount
		case balances <- balance:
		}
	}
}

func init() {
	go teller() //start monitor goroutine
}

func main() {
	var wg sync.WaitGroup
	// transactions on a joint bank account

	// Alice
	wg.Add(1)
	go func() {
		defer wg.Done()
		Deposit(200)
	}()

	// Bob
	wg.Add(1)
	go func() {
		defer wg.Done()
		go Deposit(100)

	}()
	wg.Wait()
	fmt.Println("final:", Balance())
}
