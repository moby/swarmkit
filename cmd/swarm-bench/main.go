package main

import (
	"context"
	"flag"
	"log"
	"time"
)

func main() {
	log.SetFlags(0) // disable timestamps

	var (
		count   = flag.Uint64("count", 0, "Number of tasks to start for the benchmarking session")
		manager = flag.String("manager", "localhost:4242", "Specify the manager address")
		port    = flag.Int("port", 2222, "Port used by the benchmark for listening")
		ip      = flag.String("ip", "127.0.0.1", "IP of the benchmarking tool. Tasks will phone home to this address")
	)
	flag.Parse()

	if *count == 0 {
		flag.Usage()
		log.Fatal("\n--count is mandatory")
	}

	b := NewBenchmark(&Config{
		Count:   *count,
		Manager: *manager,
		IP:      *ip,
		Port:    *port,
		Unit:    time.Second,
	})
	if err := b.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
