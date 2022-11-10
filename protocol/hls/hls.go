package hls

import (
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/push-stream-service/configure"

	"github.com/push-stream-service/av"

	log "github.com/sirupsen/logrus"
)

const (
	duration = 3000
)

var (
	ErrNoPublisher         = fmt.Errorf("no publisher")
	ErrInvalidReq          = fmt.Errorf("invalid req url path")
	ErrNoSupportVideoCodec = fmt.Errorf("no support video codec")
	ErrNoSupportAudioCodec = fmt.Errorf("no support audio codec")
)

var crossdomainxml = []byte(`<?xml version="1.0" ?>
<cross-domain-policy>
	<allow-access-from domain="*" />
	<allow-http-request-headers-from domain="*" headers="*"/>
</cross-domain-policy>`)

type Server struct {
	listener net.Listener
	conns    *sync.Map
}

func NewServer() *Server {
	ret := &Server{
		conns: &sync.Map{},
	}
	go ret.checkStop()
	return ret
}

func (server *Server) Serve(listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		server.handle(w, r)
	})
	server.listener = listener

	if configure.Config.GetBool("use_hls_https") {
		http.ServeTLS(listener, mux, "server.crt", "server.key")
	} else {
		http.Serve(listener, mux)
	}

	return nil
}

func (server *Server) GetWriter(info av.Info) av.WriteCloser {
	var s *Source
	v, ok := server.conns.Load(info.Key)
	if !ok {
		log.Debug("new hls source")
		s = NewSource(info)
		server.conns.Store(info.Key, s)
	} else {
		s = v.(*Source)
	}
	return s
}

func (server *Server) getConn(key string) *Source {
	v, ok := server.conns.Load(key)
	if !ok {
		return nil
	}
	return v.(*Source)
}

func (server *Server) checkStop() {
	for {
		<-time.After(5 * time.Second)

		server.conns.Range(func(key, val interface{}) bool {
			v := val.(*Source)
			if !v.Alive() && !configure.Config.GetBool("hls_keep_after_end") {
				log.Debug("check stop and remove: ", v.Info())
				server.conns.Delete(key)
			}
			return true
		})
	}
}

func (server *Server) handle(w http.ResponseWriter, r *http.Request) {
	if path.Base(r.URL.Path) == "crossdomain.xml" {
		w.Header().Set("Content-Type", "application/xml")
		w.Write(crossdomainxml)
		return
	}
	//s3 := s3.NewS3Handle()
	switch path.Ext(r.URL.Path) {
	case ".m3u8":
		fmt.Printf("have new request m3u8 with full path: %v \n", r.URL.Path)
		key, _ := server.parseM3u8(r.URL.Path)
		fmt.Printf("key = %v \n", key)

		conn := server.getConn(key)
		if conn == nil {
			http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
			return
		}

		tsCache := conn.GetCacheInc()
		//fmt.Printf("get m3u8, key = %v, tsCache = %v \n", key, tsCache)
		if tsCache == nil {
			http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
			return
		}
		body, err := tsCache.GenM3U8PlayList()
		//errUL := s3.UploadFileFrRam("movie.m3u8", body)
		//if errUL != nil {
		//	fmt.Println("upload to s3 error")
		//} else {
		//	fmt.Println("upload to s3 success")
		//}

		if err != nil {
			log.Debug("GenM3U8PlayList error: ", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "application/x-mpegURL")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		fmt.Printf("write m3u8 body: \n")
		w.Write(body)
	case ".ts":
		fmt.Printf("have new request ts with full path: %v \n", r.URL.Path)
		key, _ := server.parseTs(r.URL.Path)
		fmt.Println("key = %v", key)
		conn := server.getConn(key)
		if conn == nil {
			http.Error(w, ErrNoPublisher.Error(), http.StatusForbidden)
			return
		}

		tsCache := conn.GetCacheInc()
		lm := tsCache.GetLm()
		for k, _ := range lm {
			fmt.Printf("key = %v, key of ts Cache = %v \n", key, k)
		}

		item, err := tsCache.GetItem(r.URL.Path)
		if err != nil {
			log.Debug("GetItem error: ", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "video/mp2ts")
		w.Header().Set("Content-Length", strconv.Itoa(len(item.Data)))
		fmt.Printf("write item.Data body, name = %v, SeqNum=  %v,  Duration = %v \n", item.Name, item.SeqNum, item.Duration)
		//
		//errUL := s3.UploadFileFrRam(item.Name, item.Data)
		//if errUL != nil {
		//	fmt.Println("upload tls to s3 error")
		//} else {
		//	fmt.Println("upload tls to s3 success")
		//}

		w.Write(item.Data)
	}
}

// todo : wire go routine , follow input fomat and push to s3
// clear name folder for my rule
// get and stream success, replace fc func (server *Server) handle(w http.ResponseWriter, r *http.Request)

func (server *Server) DumpM3u8() {
	urlPath := "/live/movie.m3u8"
	go func() {
		//dump m3u8
		for {
			fmt.Printf("dump - have new request m3u8 with full path: %v \n", urlPath)
			fmt.Printf("dump M3u8 - 1 \n")
			key, _ := server.parseM3u8(urlPath)
			fmt.Printf("dump M3u8 - 2 \n")
			fmt.Printf("key = %v ", key)
			if server.conns == nil {
				fmt.Println("empty map")
			}

			conn := server.getConn(key)
			fmt.Printf("dump M3u8 - 3 \n")
			if conn == nil {
				fmt.Println("http error")
				continue
			}
			tsCache := conn.GetCacheInc()
			fmt.Printf("dump M3u8 - 4 \n")
			if tsCache == nil {
				fmt.Println("tsCache nill")
				continue
			}
			_, err := tsCache.GenM3U8PlayList()
			fmt.Printf("dump M3u8 - 5 \n")
			if err != nil {
				log.Debug("GenM3U8PlayList error: ", err)
				fmt.Println("GenM3U8PlayList error: ", err)
				continue
			}
			fmt.Printf("dump M3u8 - 6 ennd \n")

			fmt.Printf("GenM3U8PlayList success")

			time.Sleep(3 * time.Second)
		}
	}()
}

func (server *Server) DumpTs() {
	urlPath := "/live/movie.m3u8"
	go func() {
		for {
			fmt.Printf("dump - have new request ts with full path: %v \n", urlPath)
			key, err := server.parseM3u8(urlPath)
			if err != nil {
				fmt.Printf("get key error : %v", err.Error())
			}

			fmt.Printf("dump Ts - 1, key = %v \n", key)
			conn := server.getConn(key)
			if conn == nil {
				return
			}

			fmt.Printf("dump Ts - 2 \n")
			tsCache := conn.GetCacheInc()
			fmt.Printf("dump Ts - 3 \n")
			lm := tsCache.GetLm()
			fmt.Printf("dump Ts - 4 \n")
			for k, _ := range lm {
				fmt.Printf("key = %v, key of ts Cache = %v \n", key, k)
			}
			fmt.Printf("dump Ts - 5 \n")

			fmt.Println("start dump all file ts")
			for k, _ := range lm {
				fmt.Printf("key = %v, key of ts Cache = %v \n", key, k)
			}
			fmt.Printf("dump Ts - 6 \n")
			fmt.Println("end dump all file ts")
			time.Sleep(1 * time.Second)
		}
	}()
}

func (serve *Server) GetConnect() *sync.Map {
	return serve.conns
}

func (server *Server) parseM3u8(pathstr string) (key string, err error) {
	pathstr = strings.TrimLeft(pathstr, "/")
	key = strings.Split(pathstr, path.Ext(pathstr))[0]
	return
}

func (server *Server) parseTs(pathstr string) (key string, err error) {
	pathstr = strings.TrimLeft(pathstr, "/")
	paths := strings.SplitN(pathstr, "/", 3)
	if len(paths) != 3 {
		err = fmt.Errorf("invalid path=%s", pathstr)
		return
	}
	key = paths[0] + "/" + paths[1]

	return
}
