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

// 9.1 exercise
// can i withdraw or not due to sufficient funds?

type withdrawal struct {
	amount int
	result chan bool
}

var withdrawals = make(chan withdrawal)

// goroutines can't return values directly to each other, so you pass a channel as a callback mechanism.
// that's return_ch
// which i pass to the teller monitor, using the withdrawals channel
func Withdraw(amount int) bool {
	return_ch := make(chan bool)
	withdrawals <- withdrawal{amount, return_ch}
	return <-return_ch
}

func teller() {
	var balance int // confined to teller goroutine, so solves concurrent access problem
	for {
		select {
		case amount := <-deposits:
			balance += amount
		// this is how we receive, check, and return info. wd is the withdrawal struct.
		case wd := <-withdrawals:
			if wd.amount <= balance {
				balance -= wd.amount
				wd.result <- true
			} else {
				wd.result <- false
			}

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
		// i had an extra go routine
		Deposit(100)

	}()
	wg.Wait()
	fmt.Println("final:", Balance())

	// test 9.1 exercise
	Deposit(200)
	fmt.Println("balance after deposit:", Balance())

	ok := Withdraw(50)
	fmt.Println("withdraw 50:", ok, "balance:", Balance())

	ok = Withdraw(500)
	fmt.Println("withdraw 500:", ok, "balance:", Balance())

}

// Cake example - serially confining a var to protect it
type Cake struct{ state string }

func baker(cooked chan<- *Cake) {
	for {
		cake := new(Cake)
		cake.state = "cooked"
		cooked <- cake // cake is taken away from baker
	}
}

func icer(iced chan<- *Cake, cooked <-chan *Cake) {
	for cake := range cooked {
		cake.state = "iced"
		iced <- cake // cake is taken away from icer
	}
}
