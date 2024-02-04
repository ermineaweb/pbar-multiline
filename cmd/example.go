package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/ermineaweb/pbar"
)

func main() {
	var wg sync.WaitGroup
	pb := pbar.NewMultilineProgressBar(8)

	for i := 1; i <= pb.Total; i++ {
		wg.Add(1)
		go func(index int) {
			fmt.Println("start working")
			defer wg.Done()
			defer pb.Add(1)
			work(index)
		}(i)
	}

	wg.Wait()
}

func work(i int) {
	rnd := rand.Intn(6000) + 1000
	time.Sleep(time.Duration(rnd) * time.Millisecond)
	fmt.Printf("work done %v in %vs\n", i, rnd)
}
