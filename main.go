package main

func main() {
	freedomDht := NewDHT()
	defer freedomDht.ShutdownDHT()

	cache := NewMemoryCache()
	StartHTTPServer(freedomDht, cache)
}
