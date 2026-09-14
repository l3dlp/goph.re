package cli

import (
	"fmt"
	"gophre/pkg/service"
	"gophre/pkg/web"
	"os"
	"strconv"
)

func ParseCommand() {
	if len(os.Args) > 1 && os.Args[1] == "reset" {
		service.Reset()
	} else if len(os.Args) > 1 && os.Args[1] == "update" {
		service.Update()
	} else if len(os.Args) > 1 && os.Args[1] == "serve" {
		if len(os.Args) > 2 {
			port, err := strconv.Atoi(os.Args[2])
			if err != nil || port < 1 || port > 65535 {
				fmt.Printf("Invalid port: %s\n", os.Args[2])
				return
			}
			web.Serve(port)
		} else {
			web.Serve()
		}
	} else {
		fmt.Println("")
		fmt.Println("Usage: gophre <command>")
		fmt.Println("- gophre update         : Update RSS feeds")
		fmt.Println("- gophre serve <port>   : Run Local Web Server (port is optional)")
		fmt.Println("- gophre reset          : Reset all data")
		fmt.Println("")
	}
}
