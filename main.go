package main

func main() {
	BootstrapDHT()
	cache := NewMemoryCache()
	StartHTTPServer(cache)
}
