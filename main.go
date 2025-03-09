package main

func main() {
	freedomDht := NewDHT()
	defer freedomDht.Shutdown()

	cache := NewMemoryCache()
	StartHTTPServer(freedomDht, cache)
}
