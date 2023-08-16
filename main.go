package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/tracker"
	"github.com/sprt/byt"
)

const (
	noise         = 0.3
	retryInterval = 30 * time.Second
)

var (
	downSpeed, upSpeed byt.Size

	announceURL  string
	hash, peerID metainfo.Hash
	size         byt.Size

	complete, stall bool
	down, up        byt.Size
	interval        <-chan time.Time
	lastResp        time.Time
	nextInterval    = 10 * time.Minute
)

func init() {
	flag.Usage = usage
	flag.Var(&upSpeed, "up", "upload speed")

	log.SetFlags(0)
	log.SetOutput(new(logWriter))

	rand.Seed(time.Now().UnixNano())
}

func main() {
	flag.Parse()
	if upSpeed == 0 || flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	meta, err := metainfo.LoadFromFile(flag.Arg(0))
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	_, err = rand.Read(peerID[:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	announceURL = meta.Announce
	hash = meta.HashInfoBytes()
	info := meta.UnmarshalInfo()
	size = byt.Size(info.TotalLength())

	log.Printf("Torrent name: %s", info.Name)
	log.Printf("Torrent size: %.2f", size.Binary())

	announce(tracker.Started)
loop:
	for {
		select {
		case <-interval:
			announce(tracker.None)
		case <-interrupt:
			break loop
		}
	}
	log.Println("Quitting...")
	announce(tracker.Stopped)
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s -down <speed> -up <speed> <torrent file>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "%s", byt.FlagUsage)
}

func announce(event tracker.AnnounceEvent) {
	if !lastResp.IsZero() && !stall {
		elapsed := time.Since(lastResp).Seconds()
		down = min(size, down+byt.Size(elapsed*fuzz(downSpeed)))
		if down == size && !complete {
			event = tracker.Completed
			complete = true
		}
		up += byt.Size(elapsed * fuzz(upSpeed))
	}

	log.Printf("Announce: %.2f downloaded, %.2f uploaded", down.Binary(), up.Binary())
	req := &tracker.AnnounceRequest{
		InfoHash:   hash,
		PeerId:     peerID,
		Downloaded: int64(down),
		Left:       uint64(size - down),
		Uploaded:   int64(up),
		Event:      event,
		NumWant:    -1,
	}
	_, err := tracker.Announce(announceURL, req)
	lastResp = time.Now()
	if err != nil {
		if event == tracker.Stopped {
			log.Println("Announce error")
			interval = nil
			return
		}
		log.Println("Announce error, retrying in", retryInterval)
		interval = time.After(retryInterval)
		return
	}

	if event == tracker.Stopped {
		interval = nil
		return
	}
	log.Println("Next announce:", time.Now().Add(nextInterval).Format(time.Kitchen))
	interval = time.After(nextInterval)
}

func fuzz(n byt.Size) float64 {
	return float64(n) + (rand.Float64()-0.5)*2*noise*float64(n)
}

func min(a, b byt.Size) byt.Size {
	if a < b {
		return a
	}
	return b
}

type logWriter struct{}

func (writer logWriter) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().Format(time.Kitchen) + " " + string(bytes))
}
