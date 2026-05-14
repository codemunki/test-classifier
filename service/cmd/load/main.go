// load streams sample_data.jsonl into a running service via POST /events.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", "http://localhost:8080", "service base URL")
	file := flag.String("file", "data/sample_data.jsonl", "path to JSONL file")
	flag.Parse()

	f, err := os.Open(*file)
	if err != nil {
		log.Fatalf("open %s: %v", *file, err)
	}
	defer f.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	url := *addr + "/events"

	var sent, skipped int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		resp, err := client.Post(url, "application/json", bytes.NewReader(line))
		if err != nil {
			log.Printf("POST failed: %v", err)
			skipped++
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			log.Printf("unexpected status %d for line: %s", resp.StatusCode, line)
			skipped++
			continue
		}
		sent++
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("scan: %v", err)
	}
	fmt.Printf("done: %d sent, %d skipped\n", sent, skipped)
}
