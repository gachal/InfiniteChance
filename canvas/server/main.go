// Command canvas-server runs the canvas persistence and task-orchestration API.
package main

import "github.com/gachal/InfiniteChance/internal/app"

func main() {
	app.Run("canvas", "8081")
}
