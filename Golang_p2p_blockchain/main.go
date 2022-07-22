package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"main/block"
	"main/p2p"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	net "github.com/libp2p/go-libp2p-net"

	golog "github.com/ipfs/go-log/v2"
	ma "github.com/multiformats/go-multiaddr"
)

var mutex = &sync.Mutex{}
var Blockchain []block.Block

func readData(rw *bufio.ReadWriter) {

	for {
		str, err := rw.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		if str == "" {
			return
		}
		if str != "\n" {

			chain := make([]block.Block, 0)
			if err := json.Unmarshal([]byte(str), &chain); err != nil {
				log.Fatal(err)
			}

			mutex.Lock()
			if len(chain) > len(Blockchain) {
				Blockchain = chain
				bytes, err := json.MarshalIndent(Blockchain, "", "  ")
				if err != nil {

					log.Fatal(err)
				}
				// Yeşil konsol rengi:	\x1b[32m
				// Konsol rengini sıfırla: 	\x1b[0m
				fmt.Printf("\x1b[32m%s\x1b[0m> ", string(bytes))
			}
			mutex.Unlock()
		}
	}
}

func writeData(rw *bufio.ReadWriter) {

	go func() {
		for {
			time.Sleep(5 * time.Second)
			mutex.Lock()
			bytes, err := json.Marshal(Blockchain)
			if err != nil {
				log.Println(err)
			}
			mutex.Unlock()

			mutex.Lock()
			rw.WriteString(fmt.Sprintf("%s\n", string(bytes)))
			rw.Flush()
			mutex.Unlock()

		}
	}()

	stdReader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		sendData, err := stdReader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		sendData = strings.Replace(sendData, "\n", "", -1)
		bpm, err := strconv.Atoi(sendData)
		if err != nil {
			log.Fatal(err)
		}
		newBlock := block.GenerateBlock(Blockchain[len(Blockchain)-1], bpm)

		if block.IsBlockValid(newBlock, Blockchain[len(Blockchain)-1]) {
			mutex.Lock()
			Blockchain = append(Blockchain, newBlock)
			mutex.Unlock()
		}

		bytes, err := json.Marshal(Blockchain)
		if err != nil {
			log.Println(err)
		}

		spew.Dump(Blockchain)

		mutex.Lock()
		rw.WriteString(fmt.Sprintf("%s\n", string(bytes)))
		rw.Flush()
		mutex.Unlock()
	}

}

func handleStream(s net.Stream) {

	log.Println("Yeni bir akış var!")

	// Engellemeyen okuma ve yazma için bir arabellek akışı oluşturun.
	rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))

	go readData(rw)
	go writeData(rw)

	// stream 's' siz kapatana kadar (veya diğer taraf kapatana kadar) açık kalacaktır.
}
func main() {
	t := time.Now()
	genesisBlock := block.Block{}
	genesisBlock = block.Block{Index: 0, Timestamp: t.String(), BPM: 0, Hash: block.CalculateHash(genesisBlock), PrevHash: ""}

	Blockchain = append(Blockchain, genesisBlock)
	//ctx, cancel := context.WithCancel(context.Background())
	//defer cancel()

	// LibP2P kodu, mesajları günlüğe kaydetmek için golog kullanır. Farklı ile giriş yapıyorlar
	// dize kimlikleri (yani "swarm"). için ayrıntı düzeyini kontrol edebiliriz
	// ile tüm kaydediciler:
	golog.SetAllLoggers(golog.LevelInfo) // Ek bilgi için INFO olarak değiştirin

	// Komut satırından seçenekleri ayrıştırın
	listenF := flag.Int("l", 0, "gelen bağlantıları bekle")
	target := flag.String("d", "", "çevirmek için hedef eş")
	secio := flag.Bool("secio", false, "secio'yu etkinleştir")
	seed := flag.Int64("seed", 0, "kimlik üretimi için rastgele ayarla")
	flag.Parse()

	if *listenF == 0 {
		log.Fatal("Lütfen -l ile bağlanacak bir bağlantı noktası sağlayın")
	}

	// Verilen çoklu adresi dinleyen bir ana bilgisayar yapın
	ha, err := p2p.MakeBasicHost(*listenF, *secio, *seed)
	if err != nil {
		log.Fatal(err)
	}

	if *target == "" {
		log.Println("listening for connections")
		// Ana bilgisayar A'da bir akış işleyici ayarlayın. /p2p/1.0.0
		// kullanıcı tanımlı bir protokol adı.
		ha.SetStreamHandler("/p2p/1.0.0", handleStream)

		select {} // sonsuza kadar asmak
		/**** Dinleyici kodunun bittiği yer burasıdır. ****/
	} else {
		ha.SetStreamHandler("/p2p/1.0.0", handleStream)

		// Aşağıdaki kod, hedefin eş kimliğini
		//verilen çoklu adres
		ipfsaddr, err := ma.NewMultiaddr(*target)
		if err != nil {
			log.Fatalln(err)
		}

		pid, err := ipfsaddr.ValueForProtocol(ma.P_IPFS)
		if err != nil {
			log.Fatalln(err)
		}

		peerid, err := peer.Decode(pid)
		if err != nil {
			log.Fatalln(err)
		}

		// /ipfs/<peerID> kısmını hedeften ayırın
		// /ip4/<a.b.c.d>/ipfs/<peer> /ip4/<a.b.c.d> olur
		targetPeerAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ipfs/%s", pid))

		targetAddr := ipfsaddr.Decapsulate(targetPeerAddr)

		// Bir eş kimliğimiz ve bir hedefAddr'miz var, bu yüzden onu eş deposuna ekliyoruz
		// böylece LibP2P onunla nasıl iletişim kuracağını biliyor
		ha.Peerstore().AddAddr(peerid, targetAddr, peerstore.PermanentAddrTTL)

		log.Println("opening stream")
		// ana bilgisayar B'den ana bilgisayar A'ya yeni bir akış yapın
		// yukarıda belirlediğimiz işleyici tarafından ana bilgisayar A'da ele alınmalıdır çünkü
		// aynı /p2p/1.0.0 protokolünü kullanıyoruz
		s, err := ha.NewStream(context.Background(), peerid, "/p2p/1.0.0")
		if err != nil {
			log.Fatalln(err)
		}
		// Okuma ve yazma işlemlerinin engellenmemesi için arabelleğe alınmış bir akış oluşturun.
		rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))

		//Verileri okumak ve yazmak için bir iş parçacığı oluşturun.
		go writeData(rw)
		go readData(rw)

		select {} // sonsuza kadar asmak

	}
}
