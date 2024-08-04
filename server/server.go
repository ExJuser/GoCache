package main

import (
	"context"
	"flag"
	"github.com/ExJuser/GoCache"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	port    int
	logfile string

	cache  *GoCache.GoCache
	config = GoCache.Config{}
)

func init() {
	flag.BoolVar(&config.Verbose, "v", false, "Verbose logging.")
	flag.IntVar(&config.Shards, "shards", 1024, "Number of shards for the cache.")
	flag.IntVar(&config.MaxEntriesInWindow, "maxInWindow", 1000*10*60, "Used only in initial memory allocation.")
	flag.DurationVar(&config.LifeWindow, "lifetime", 100000*100000*60, "Lifetime of each cache object.")
	flag.IntVar(&config.HardMaxCacheSize, "max", 8192, "Maximum amount of data in the cache in MB.")
	flag.IntVar(&config.MaxEntrySize, "maxShardEntrySize", 500, "The maximum size of each object stored in a shard. Used only in initial memory allocation.")
	flag.IntVar(&port, "port", 9090, "The port to listen on.")
	flag.StringVar(&logfile, "logfile", "", "Location of the logfile.")
}

func main() {
	flag.Parse()

	var logger *log.Logger

	if logfile == "" {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	} else {
		f, err := os.OpenFile(logfile, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		logger = log.New(f, "", log.LstdFlags)
	}

	var err error
	cache, err = GoCache.New(context.Background(), config)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Print("cache initialised.")

	r := gin.Default()
	r.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Printf("%s request to %s took %vns.", c.Request.Method, c.Request.URL, time.Since(start).Nanoseconds())
	})

	r.GET("/api/v1/cache/:key", getCacheHandler)
	r.PUT("/api/v1/cache/:key", putCacheHandler)
	r.DELETE("/api/v1/cache/:key", deleteCacheHandler)
	r.GET("/api/v1/stats", func(c *gin.Context) {

	})

	strPort := ":" + strconv.Itoa(port)
	logger.Printf("starting server on :%d", port)
	if err = r.Run(strPort); err != nil {
		logger.Print("server start failed.")
	}
}

func getCacheHandler(ctx *gin.Context) {
	target := ctx.Param("key")
	entry, err := cache.Get(target)
	if err != nil {
		errMsg := (err).Error()
		if strings.Contains(errMsg, "not found") {
			log.Print(err)
			ctx.JSON(http.StatusOK, gin.H{
				"code":  http.StatusNotFound,
				"value": "缓存不存在",
			})
			return
		}
		log.Print(err)
		ctx.JSON(http.StatusOK, gin.H{
			"code":  http.StatusInternalServerError,
			"value": "服务器内部错误",
		})
		return
	}
	log.Printf("成功读取缓存：\"%s\":\"%s\"", target, string(entry))
	ctx.JSON(http.StatusOK, gin.H{
		"code":  http.StatusOK,
		"value": string(entry),
	})
}

func putCacheHandler(ctx *gin.Context) {
	target := ctx.Param("key")
	entry, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		log.Print(err)
		ctx.JSON(http.StatusOK, gin.H{
			"code":  http.StatusInternalServerError,
			"value": "服务器内部错误",
		})
	}
	if err = cache.Set(target, entry); err != nil {
		log.Print(err)
		ctx.JSON(http.StatusOK, gin.H{
			"code":  http.StatusInternalServerError,
			"value": "服务器内部错误",
		})
	}
	log.Printf("成功存储\"%s\":\"%s\"", target, string(entry))
	ctx.JSON(http.StatusOK, gin.H{
		"code":  http.StatusCreated,
		"value": "缓存设置成功",
	})
}

func deleteCacheHandler(ctx *gin.Context) {
	target := ctx.Param("key")
	if err := cache.Delete(target); err != nil {
		if strings.Contains((err).Error(), "not found") {
			ctx.JSON(http.StatusOK, gin.H{
				"code":  http.StatusNotFound,
				"value": "缓存不存在",
			})
			log.Printf("%s不存在", target)
			return
		}
		ctx.JSON(http.StatusOK, gin.H{
			"code":  http.StatusInternalServerError,
			"value": "服务器内部错误",
		})
		log.Printf("服务器内部错误：%s", err)
	}
	ctx.JSON(http.StatusOK, gin.H{
		"code":  http.StatusOK,
		"value": "缓存删除成功",
	})
}
