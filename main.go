package main

import (
	"container/list"
	"flag"
	"fmt"
	ui "github.com/gizak/termui"
	"github.com/kofemann/nfstop/nfs"
	"github.com/kofemann/nfstop/sniffer"
	"github.com/kofemann/nfstop/stream"
	"github.com/kofemann/nfstop/utils"
	"github.com/tsg/gopacket"
	"github.com/tsg/gopacket/layers"
	"github.com/tsg/gopacket/pcap"
	"os"
	"time"
)

const (
	// ANY_DEVICE peseudo interface to listen all interfaces
	ANY_DEVICE = "any"

	// NFS_FILTER default packet fiter to capture nfs traffic
	NFS_FILTER = "port 2049"

	// SNAPLEN packet snapshot length
	SNAPLEN = 65535

	REFRESH_TIME = time.Second * 2
)

var streams = make(map[string]*stream.TcpStream)

var iface = flag.String("i", ANY_DEVICE, "name of `interface` to listen")
var filter = flag.String("f", NFS_FILTER, "capture `filter` in libpcap filter syntax")
var listInterfaces = flag.Bool("D", false, "print list of interfaces and exit")
var snaplen = flag.Int("s", SNAPLEN, "packet `snaplen` - snapshot length")

func main() {

	flag.Parse()

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

	sniffer := &sniffer.Sniffer{
		Interface: *iface,
		Filter:    *filter,
		Snaplen:   *snaplen,
	}

	handle, err := sniffer.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize sniffer: %v\n", err)
		os.Exit(1)
	}

	// Read in packets, pass to assembler.

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	packets := packetSource.Packets()

	counter := 0
	ticker := time.Tick(REFRESH_TIME)

	collector := list.New()

	quit := make(chan int)

	// request selection

	ByClient := func(r *nfs.NfsRequest) string {
		return r.GetClient()
	}

	ByServer := func(r *nfs.NfsRequest) string {
		return r.GetServer()
	}

	ByOpCode := func(r *nfs.NfsRequest) string {
		return r.GetOpCode()
	}

	ByUser := func(r *nfs.NfsRequest) string {
		return r.GetCred()
	}

	selection := ByClient

	// UI
	err = ui.Init()
	if err != nil {
		panic(err)
	}
	defer ui.Close()

	title := ui.NewPar(":PRESS q TO QUIT")
	title.TextFgColor = ui.ColorWhite
	title.BorderLabel = "NFS top"
	title.BorderFg = ui.ColorCyan
	title.Height = 3

	labelList := ui.NewList()
	labelList.Border = false
	labelList.Height = ui.TermHeight()

	histogramList := ui.NewList()
	histogramList.Border = false
	histogramList.Height = ui.TermHeight()

	valuesList := ui.NewList()
	valuesList.Border = false
	valuesList.Height = ui.TermHeight()

	ui.Body.Rows = make([]*ui.Row, 0)
	ui.Body.AddRows(
		ui.NewRow(
			ui.NewCol(12, 0, title),
		),
		ui.NewRow(
			ui.NewCol(4, 0, labelList),
			ui.NewCol(6, 0, histogramList),
			ui.NewCol(2, 0, valuesList),
		),
	)

	go func() {
		for {
			select {
			case <-quit:
				return
			case <-ticker:
				// time to refresh screen
				l := collector
				collector = list.New()

				term := utils.Aggr(l, selection)
				sum := term.Sum()
				labels := make([]string, l.Len())
				histograms := make([]string, l.Len())
				values := make([]string, l.Len())
				size := (ui.TermWidth() / 12) * 8

				for i, e := range term.Elements {
					labels[i] = fmt.Sprintf("%8s", e.Key)
					histograms[i] = utils.FillHisto(sum, e.Value, size)
					values[i] = fmt.Sprintf("%8d", e.Value)
				}

				labelList.Items = labels
				histogramList.Items = histograms
				valuesList.Items = values
				ui.Body.Align()
				ui.Render(ui.Body)

			case packet := <-packets:
				// A nil packet indicates the end of a pcap file.
				if packet == nil {
					os.Exit(0)
				}
				counter++
				//		log.Println(packet)
				if packet.NetworkLayer() == nil || packet.TransportLayer() == nil || packet.TransportLayer().LayerType() != layers.LayerTypeTCP {

					continue
				}

				tcp := packet.TransportLayer().(*layers.TCP)

				data := tcp.Payload
				if len(data) == 0 {
					continue
				}

				// direction independed unique connection identifier
				connectionKey := fmt.Sprintf("%d:%d",
					packet.NetworkLayer().NetworkFlow().FastHash(),
					tcp.TransportFlow().FastHash(),
				)

				dir := 0
				tcpStream, ok := streams[connectionKey]
				if !ok {
					tcpStream = stream.NewTcpStream(tcp)
					streams[connectionKey] = tcpStream
				} else {
					if tcpStream.SrcPort != tcp.SrcPort {
						dir = 1
					}
				}

				event := &stream.Event{
					Timestamp: packet.Metadata().CaptureInfo.Timestamp,
					Src:       packet.NetworkLayer().NetworkFlow().Src().String(),
					Dst:       packet.NetworkLayer().NetworkFlow().Dst().String(),
					SrcPort:   tcp.TransportFlow().Src().String(),
					DstPort:   tcp.TransportFlow().Dst().String(),
					Stream:    tcpStream,
				}

				rawStream := tcpStream.Data[dir]
				rawStream.Data = append(rawStream.Data, data...)

				nfs.DataArrieved(rawStream, event, collector)
			}
		}
	}()

	ui.Handle("/sys/kbd/s", func(ui.Event) {
		selection = ByServer
	})

	ui.Handle("/sys/kbd/o", func(ui.Event) {
		selection = ByOpCode
	})

	ui.Handle("/sys/kbd/c", func(ui.Event) {
		selection = ByClient
	})

	ui.Handle("/sys/kbd/u", func(ui.Event) {
		selection = ByUser
	})

	ui.Handle("/sys/kbd/q", func(ui.Event) {
		ui.StopLoop()
		quit <- 1
	})

	ui.Handle("/sys/wnd/resize", func(e ui.Event) {
		ui.Body.Width = ui.TermWidth()
		ui.Body.Align()
		ui.Clear()
		ui.Render(ui.Body)
	})

	ui.Body.Align()
	ui.Render(ui.Body)

	ui.Loop()
}
