// Command gateway-server runs the OpenAI-compatible token API gateway.
package main

import "github.com/gachal/InfiniteChance/internal/app"

func main() {
	app.Run("gateway", "8080")
}
