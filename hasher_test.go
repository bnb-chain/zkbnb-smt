package bsmt

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestHasher_Hash(t *testing.T) {
	pool := NewHasherPool(func() hash.Hash {
		return sha256.New()
	})

	bytes := pool.Hash([]byte{2})
	fmt.Println(bytes)
}

func Test_main(t *testing.T) {

	msg_chan := make(chan string)
	done := make(chan bool)
	i := 0

	go func() {
		for {
			i++
			time.Sleep(1 * time.Second)
			msg_chan <- "on message"
			<-done
		}
	}()

	go func() {
		for {
			select {
			case msg := <-msg_chan:
				i++
				fmt.Println(msg + " " + strconv.Itoa(i))
				time.Sleep(2 * time.Second)
				done <- true
			}
		}

	}()

	time.Sleep(20 * time.Second)
}

func Test_main2(t *testing.T) {
	ch := make(chan bool, 10)

	data := make([]int, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() {
				if err := recover(); err != nil {
					ch <- false
				}
			}()
			data[idx] = idx
			ch <- true
		}(i)
	}
	for j := 0; j < 10; j++ {
		fmt.Print(<-ch, " ")
	}
	fmt.Println(data)
}

func sum() int {
	s := 0
	var wg sync.WaitGroup
	var mutex sync.Mutex
	wg.Add(10)
	for i := 1; i <= 10; i++ {
		go func(i int) {
			mutex.Lock()
			s += i
			mutex.Unlock()
			wg.Done()
		}(i)
	}
	wg.Wait()
	return s
}
func TestMain3(t *testing.T) {
	//执行100次结果都是55
	for i := 0; i < 100; i++ {
		fmt.Print(sum(), " ")
	}
}

func sum2() int {
	ch := make(chan int)
	for i := 1; i <= 10; i++ {
		go func(i int) {
			ch <- i
		}(i)
	}
	s := 0
	for i := 0; i < 10; i++ {
		s += <-ch
	}
	return s
}

func Test_main4(t *testing.T) {
	for i := 0; i < 100; i++ {
		fmt.Print(sum2(), " ")
	}
}
