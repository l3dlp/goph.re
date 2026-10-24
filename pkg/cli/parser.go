package cli

import (
	"fmt"
	"gophre/pkg/push"
	"gophre/pkg/service"
	"gophre/pkg/web"
	"os"
	"strconv"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func ParseCommand() {
	if len(os.Args) > 1 && os.Args[1] == "reset" {
		service.Reset()
	} else if len(os.Args) > 1 && os.Args[1] == "update" {
		service.Update()
	} else if len(os.Args) > 1 && os.Args[1] == "vapid" {
		private, public, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			fmt.Printf("Error generating VAPID keys: %v.\n", err)
			return
		}
		fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", public, private)
	} else if len(os.Args) > 2 && os.Args[1] == "push" {
		url := "/"
		if len(os.Args) > 4 {
			url = os.Args[4]
		}
		body := ""
		if len(os.Args) > 3 {
			body = os.Args[3]
		}
		sent := push.Broadcast(func(push.Subscription) (push.Payload, bool) {
			return push.Payload{Title: os.Args[2], Body: body, URL: url, Tag: "gophre-announce"}, true
		})
		fmt.Printf("Sent to %d of %d subscribers.\n", sent, push.Count())
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
		fmt.Println("- gophre vapid          : Generate Web Push (VAPID) keys for .env")
		fmt.Println("- gophre push <title> [body] [url] : Notify every push subscriber")
		fmt.Println("")
	}
}
