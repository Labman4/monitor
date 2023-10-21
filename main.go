package main

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		clientIP := getClientIP(c.Request)
		c.JSON(http.StatusOK, gin.H{"client_ip": clientIP})
	})

	r.Run(":11415")
}

func getClientIP(req *http.Request) string {
	clientIP := req.Header.Get("X-Real-IP") 
	if clientIP == "" {
		clientIP = req.Header.Get("X-Forwarded-For")
	}
	if clientIP == "" {
		clientIP = req.RemoteAddr
	}
	host, _, _ := net.SplitHostPort(clientIP)
	return host
}