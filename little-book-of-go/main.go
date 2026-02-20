package main

import (
	"fmt"
)

func main() {
	goku := &Saiyan{"Goku", 1000}
	goku.Super()
	fmt.Println(goku.Power)

}

type Saiyan struct {
	Name  string
	Power int
}

func (s *Saiyan) Super() {
	s.Power += 10000
}

func NewSaiyan(name string, power int) *Saiyan {
	return &Saiyan{
		Name:  name,
		Power: power,
	}
}
