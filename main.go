package main

import (
	"flag"
	"fmt"
	"github.com/tsg/gopacket"
	"os"
	"syscall"
	//	"github.com/tsg/gopacket/layers"
	"github.com/kofemann/nfstop/sniffer"
	"github.com/tsg/gopacket/pcap"
)

const (
	// ANY_DEVICE peseudo interface to listen all interfaces
	ANY_DEVICE = "any"

	// NFS_FILTER default packet fiter to capture nfs traffic
	NFS_FILTER = "port 2049"

	// SNAPLEN packet snapshot length
	SNAPLEN = 65535
)

type NopWorker struct{}

func (w *NopWorker) OnPacket(data []byte, ci *gopacket.CaptureInfo) {

}

var iface = flag.String("i", ANY_DEVICE, "name of `interface` to listen")
var filter = flag.String("f", NFS_FILTER, "capture `filter` in libpcap filter syntax")
var listInterfaces = flag.Bool("D", false, "print list of interfaces and exit")
var snaplen = flag.Int("s", SNAPLEN, "packet `snaplen` - snapshot length")

func main() {

	flag.Parse()

	sniffer := &sniffer.Sniffer{
		Interface: *iface,
		Filter:    *filter,
		Snaplen:   *snaplen,
		Worker:    &NopWorker{},
	}
	counter := 0

	err := sniffer.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize sniffer: %v\n", err)
		os.Exit(1)
	}

	if *listInterfaces {
		ifaces, err := pcap.FindAllDevs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get list of interfaces: %v\n", err)
			os.Exit(1)
		}

		for i, dev := range ifaces {
			fmt.Printf("%d. %s\n", i+1, dev.Name)
		}
		os.Exit(0)
	}

	isDone := false
	for !isDone {

		data, ci, err := sniffer.DataSource.ReadPacketData()

		if err == pcap.NextErrorTimeoutExpired || err == syscall.EINTR {
			// no packet received
			continue
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Sniffing error: %s\n", err)
			isDone = true
			continue
		}

		if len(data) == 0 {
			// Empty packet, probably timeout from afpacket
			continue
		}

		counter++
		fmt.Printf("Packet number: %d\n", counter)

		sniffer.Worker.OnPacket(data, &ci)
	}
}
