package healthcheck

import (
	"encoding/json"
	"fmt"
	"github.com/Dreamacro/clash/adapters/outbound"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Sansui233/proxypool/pkg/proxy"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SpeedResult proxy.Identifier string-> speedresult string
type SpeedResult map[string]string

func SpeedTest(proxies []proxy.Proxy) {

}

// speedResult: Mbit/s (not MB/s). -1 for error
func proxySpeedTest(p proxy.Proxy) (speedResult float64, err error) {
	// convert to clash proxy struct
	pmap := make(map[string]interface{})
	err = json.Unmarshal([]byte(p.String()), &pmap)
	if err != nil {
		return
	}
	pmap["port"] = int(pmap["port"].(float64))
	if p.TypeName() == "vmess" {
		pmap["alterId"] = int(pmap["alterId"].(float64))
	}

	clashProxy, err := outbound.ParseProxy(pmap)
	if err != nil {
		return -1, err
	}

	// start speedtest using speedtest.net
	// fetch server info
	var user *User
	var serverList *ServerList
	errorCh := make(chan error)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		user, err = fetchUserInfo(clashProxy)
		if err != nil {
			errorCh <- err
			return
		}
	}()
	serverList, err = fetchServerList(clashProxy)
	wg.Wait()
	close(errorCh)
	var ok bool
	if err, ok = <-errorCh; ok {
	} // clear channel cahe
	if err != nil {
		return -1, err
	}

	// Calculate distance
	for i := range serverList.Servers {
		server := &serverList.Servers[i]
		sLat, _ := strconv.ParseFloat(server.Lat, 64)
		sLon, _ := strconv.ParseFloat(server.Lon, 64)
		uLat, _ := strconv.ParseFloat(user.Lat, 64)
		uLon, _ := strconv.ParseFloat(user.Lon, 64)
		server.Distance = distance(sLat, sLon, uLat, uLon)
	}
	// Sort by distance
	sort.Sort(ByDistance{serverList.Servers})

	var targets Servers
	targets = append(serverList.Servers[:3])

	// Test
	targets.StartTest(clashProxy)
	speedResult = targets.GetResult()

	return speedResult, nil

}

/* Test with SpeedTest.net */
var dlSizes = [...]int{350, 500, 750, 1000, 1500, 2000, 2500, 3000, 3500, 4000}
var ulSizes = [...]int{100, 300, 500, 800, 1000, 1500, 2500, 3000, 3500, 4000} //kB

func pingTest(clashProxy C.Proxy, sURL string) time.Duration {
	pingURL := strings.Split(sURL, "/upload")[0] + "/latency.txt"

	l := time.Second * 10
	for i := 0; i < 2; i++ {
		sTime := time.Now()
		err := HTTPGetViaProxy(clashProxy, pingURL)
		fTime := time.Now()
		if err != nil {
			//log.Println("\n\t\t[speedtest.go] Warning: fail to test latency, This may lead to inaccuracy in speed test.\n\t\t", err)
			continue
		}
		if fTime.Sub(sTime) < l {
			l = fTime.Sub(sTime)
		}
	}
	return l / 2.0
}

func downloadTest(clashProxy C.Proxy, sURL string, latency time.Duration) float64 {
	dlURL := strings.Split(sURL, "/upload")[0]
	fmt.Printf("Download Test: ")
	wg := new(sync.WaitGroup)

	// Warming up
	sTime := time.Now()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go dlWarmUp(clashProxy, wg, dlURL)
	}
	wg.Wait()
	fTime := time.Now()
	// 1.125MB for each request (750 * 750 * 2)
	wuSpeed := 1.125 * 8 * 2 / fTime.Sub(sTime.Add(latency)).Seconds()

	// Decide workload by warm up speed. Weight is the level of size.
	workload := 0
	weight := 0
	if 10.0 < wuSpeed {
		workload = 8
		weight = 4
	} else if 4.0 < wuSpeed {
		workload = 4
		weight = 4
	} else if 2.5 < wuSpeed {
		workload = 2
		weight = 4
	} else { // if too slow, skip main test to save time
		return wuSpeed
	}

	// Main speedtest
	dlSpeed := wuSpeed
	sTime = time.Now()
	for i := 0; i < workload; i++ {
		wg.Add(1)
		go downloadRequest(clashProxy, wg, dlURL, weight)
	}
	wg.Wait()
	fTime = time.Now()

	reqMB := dlSizes[weight] * dlSizes[weight] * 2 / 1000 / 1000
	dlSpeed = float64(reqMB) * 8 * float64(workload) / fTime.Sub(sTime).Seconds()

	return dlSpeed
}

func dlWarmUp(clashProxy C.Proxy, wg *sync.WaitGroup, dlURL string) {
	size := dlSizes[2]
	url := dlURL + "/random" + strconv.Itoa(size) + "x" + strconv.Itoa(size) + ".jpg"
	HTTPGetBodyViaProxy(clashProxy, url)

	wg.Done()
}

func downloadRequest(clashProxy C.Proxy, wg *sync.WaitGroup, dlURL string, w int) {
	size := dlSizes[w]
	url := dlURL + "/random" + strconv.Itoa(size) + "x" + strconv.Itoa(size) + ".jpg"

	HTTPGetBodyViaProxy(clashProxy, url)

	wg.Done()
}
