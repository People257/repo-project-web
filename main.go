package main

import (
	"log"

	"github.com/gin-gonic/gin"
)

//TIP To run your code, right-click the code and select <b>Run</b>. Alternatively, click
// the <icon src="AllIcons.Actions.Execute"/> icon in the gutter and select the <b>Run</b> menu item from here.

func main() {
	//TIP Press <shortcut actionId="ShowIntentionActions"/> when your caret is at the underlined or highlighted text
	// to see how GoLand suggests fixing it.

	// 创建 Gin 引擎
	router := gin.Default()

	// 设置上传限制
	router.MaxMultipartMemory = maxUploadSize

	// 注册路由
	router.POST("/api/combine-code", handleCombineCodeGin)
	router.GET("/api/github-code", handleGitHubRepo)

	// 定义监听地址
	listenAddr := ":8080"

	// 启动服务器
	log.Printf("启动服务，监听地址 %s", listenAddr)
	if err := router.Run(listenAddr); err != nil {
		log.Fatalf("启动 Gin 服务失败: %v", err)
	}
}

//TIP See GoLand help at <a href="https://www.jetbrains.com/help/go/">jetbrains.com/help/go/</a>.
// Also, you can try interactive lessons for GoLand by selecting 'Help | Learn IDE Features' from the main menu.
