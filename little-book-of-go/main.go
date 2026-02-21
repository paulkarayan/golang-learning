package main

import (
	"fmt"
	"math/rand"
	"sort"
)

func main() {
	scores := make([]int, 100)
	for i := 0; i < 100; i++ {
		scores[i] = int(rand.Intn(1000))
	}
	sort.Ints(scores)

	worst := make([]int, 5)
	// copy(worst, scores[:5])
	copy(worst, scores[:7])

	fmt.Println(worst)

}

func removeAtIndex(source []int, index int) []int {
	lastIndex := len(source) - 1
	source[index], source[lastIndex] = source[lastIndex], source[index]
	return source[:lastIndex]
}
