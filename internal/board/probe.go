package board

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// probeClient gives every probe 2 s, redirects included.
var probeClient = &http.Client{Timeout: 2 * time.Second}

// Probe reports whether url answers: a HEAD (a GET when HEAD gets 405)
// with any status below 500.
func Probe(url string) bool {
	resp, err := probeClient.Head(url)
	if err == nil && resp.StatusCode == http.StatusMethodNotAllowed {
		resp.Body.Close()
		resp, err = probeClient.Get(url)
	}
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// ProbeAll probes every http(s) url of rs in parallel. The result maps url
// to up; other urls are left out (not probed).
func ProbeAll(rs []Running) map[string]bool {
	out := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range rs {
		if !strings.HasPrefix(r.URL, "http://") && !strings.HasPrefix(r.URL, "https://") {
			continue
		}
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			up := Probe(url)
			mu.Lock()
			out[url] = up
			mu.Unlock()
		}(r.URL)
	}
	wg.Wait()
	return out
}
