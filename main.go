package main

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		clientIP := c.ClientIP()
		c.String(http.StatusOK, clientIP)
	})

	r.Run(":11415")
}
