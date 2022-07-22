package p2p

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strings"

	mrand "math/rand"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	crypto "github.com/libp2p/go-libp2p-crypto"
	host "github.com/libp2p/go-libp2p-host"
	ma "github.com/multiformats/go-multiaddr"
)

//makeBasicHost, üzerinde rastgele bir eş kimliği dinleyen bir LibP2P ana bilgisayarı oluşturur.
// verilen çoklu adres. Güvensizlik varsa bağlantıyı şifrelemez.
func MakeBasicHost(listenPort int, insecure bool, randseed int64) (host.Host, error) {
	var r io.Reader
	if randseed == 0 {
		r = rand.Reader
	} else {
		r = mrand.New(mrand.NewSource(randseed))
	}

	// Bu ana bilgisayar için bir anahtar çifti oluşturun. en azından kullanacağız
	// geçerli bir ana bilgisayar kimliği elde etmek için.
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		return nil, err
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", listenPort)),
		libp2p.Identity(priv),
	}

	if insecure {
		opts = append(opts, libp2p.NoSecurity)
	}

	basicHost, err := libp2p.New(opts...)
	if err != nil {
		return nil, err
	}

	// Ana bilgisayar çoklu adresi oluşturun
	hostAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ipfs/%s", basicHost.ID().Pretty()))

	// Şimdi bu ana bilgisayara ulaşmak için tam bir çoklu adres oluşturabiliriz.
	// her iki adresi de kapsülleyerek:
	addrs := basicHost.Addrs()
	var addr ma.Multiaddr
	// "ip4" ile başlayan adresi seçin
	for _, i := range addrs {
		if strings.HasPrefix(i.String(), "/ip4") {
			addr = i
			break
		}
	}
	fullAddr := addr.Encapsulate(hostAddr)
	log.Printf("Ben %s\n", fullAddr)
	if insecure {
		log.Printf("Çalıştırın -> \"go run main.go -l %d -d %s -secio\" farklı bir terminalde\n", listenPort+1, fullAddr)
	} else {
		log.Printf("Çalıştırın -> \"go run main.go -l %d -d %s\" farklı bir terminalde\n", listenPort+1, fullAddr)
	}

	return basicHost, nil
}

func GetHostAddress(ha host.Host) string {
	// Ana bilgisayar çoklu adresi oluşturun
	hostAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ipfs/%s", ha.ID().Pretty()))

	// Şimdi bu ana bilgisayara ulaşmak için tam bir çoklu adres oluşturabiliriz.
	// her iki adresi de kapsülleyerek:
	addr := ha.Addrs()[0]
	return addr.Encapsulate(hostAddr).String()
}

func StartListener(ctx context.Context, ha host.Host, listenPort int, insecure bool) {
	fullAddr := GetHostAddress(ha)
	log.Printf("I am %s\n", fullAddr)

	// Ana bilgisayar A'da bir akış işleyici ayarlayın. /echo/1.0.0
	// kullanıcı tanımlı bir protokol adı.
	ha.SetStreamHandler("/echo/1.0.0", func(s network.Stream) {
		log.Println("Dinleyici yeni akış aldı")
		if err := DoEcho(s); err != nil {
			log.Println(err)
			s.Reset()
		} else {
			s.Close()
		}
	})

	log.Println("bağlantıları dinlemek")

	if insecure {
		log.Printf("Çalıştırın ->  \"./echo -l %d -d %s -insecure\" farklı bir terminalde\n", listenPort+1, fullAddr)
	} else {
		log.Printf("Çalıştırın -> \"./echo -l %d -d %s\" farklı bir terminalde\n", listenPort+1, fullAddr)
	}
}

func RunSender(ctx context.Context, ha host.Host, targetPeer string) {
	fullAddr := GetHostAddress(ha)
	log.Printf("Ben %s\n", fullAddr)

	// Ana bilgisayar A'da bir akış işleyici ayarlayın. /echo/1.0.0
	// kullanıcı tanımlı bir protokol adı.
	ha.SetStreamHandler("/echo/1.0.0", func(s network.Stream) {
		log.Println("gönderen yeni akış aldı")
		if err := DoEcho(s); err != nil {
			log.Println(err)
			s.Reset()
		} else {
			s.Close()
		}
	})

	// Aşağıdaki kod, hedefin eş kimliğini
	// verilen çoklu adres
	ipfsaddr, err := ma.NewMultiaddr(targetPeer)
	if err != nil {
		log.Println(err)
		return
	}

	pid, err := ipfsaddr.ValueForProtocol(ma.P_IPFS)
	if err != nil {
		log.Println(err)
		return
	}

	peerid, err := peer.Decode(pid)
	if err != nil {
		log.Println(err)
		return
	}

	// /ipfs/<peerID> kısmını hedeften ayırın
	// /ip4/<a.b.c.d>/ipfs/<peer>  /ip4/<a.b.c.d> olur
	targetPeerAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ipfs/%s", pid))
	targetAddr := ipfsaddr.Decapsulate(targetPeerAddr)

	// Bir eş kimliğimiz ve bir hedefAddr'miz var, bu yüzden onu eş deposuna ekliyoruz
	// böylece LibP2P onunla nasıl iletişim kuracağını biliyor
	ha.Peerstore().AddAddr(peerid, targetAddr, peerstore.PermanentAddrTTL)

	log.Println("sender opening stream")
	// Ana bilgisayar B'den ana bilgisayar A'ya yeni bir akış yapın
	// yukarıda belirlediğimiz işleyici tarafından ana bilgisayar A'da ele alınmalıdır çünkü
	// aynı /echo/1.0.0 protokolünü kullanıyoruz
	s, err := ha.NewStream(context.Background(), peerid, "/echo/1.0.0")
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("Gönderen merhaba diyor")
	_, err = s.Write([]byte("Merhaba!\n"))
	if err != nil {
		log.Println(err)
		return
	}

	out, err := ioutil.ReadAll(s)
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("Cevabı oku: %q\n", out)
}

// doEcho, bir akış veri satırını okur ve geri yazar
func DoEcho(s network.Stream) error {
	buf := bufio.NewReader(s)
	str, err := buf.ReadString('\n')
	if err != nil {
		return err
	}

	log.Printf("Oku: %s", str)
	_, err = s.Write([]byte(str))
	return err
}
