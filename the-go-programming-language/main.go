// ch 9 - concurrency w shared vars

package main

import "fmt"

var balance int

func Deposit(amount int) { balance = balance + amount }
func Balance() int       { return balance }

func main() {

	// transactions on a joint bank account

	// Alice
	go func() {
		Deposit(200)
		fmt.Println("=", Balance())
	}()

	// Bob
	go Deposit(100)
}
