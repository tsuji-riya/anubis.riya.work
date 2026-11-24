// Command server は静的ファイルだけを配る小さな HTTP サーバー。
//
// nginx 等の汎用サーバーをやめ、Go 標準ライブラリだけで実装することで、
// 実行イメージを scratch + 静的バイナリ 1 本 + サイトのファイルだけにする。
// OS パッケージが存在しないので、OS 由来の脆弱性は原理的に付かない。
package main

import (
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"
)

// 拡張子から決まる Content-Type の上書き。
// EmulatorJS の .wasm / .data 等、配信に必要なものだけを明示する。
var mimeTypes = map[string]string{
	".wasm": "application/wasm",
	".js":   "text/javascript; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".json": "application/json",
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
	".data": "application/octet-stream",
}

type fileServer struct {
	root *os.Root
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ct, ok := mimeTypes[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "same-origin")
	// Content-Security-Policy は付けない。EmulatorJS がインラインスクリプトと
	// blob: ワーカーを使うため、付けると壊れる。静的ファイルしか配らず、
	// サーバー側で動的コンテンツを生成しないので、リスクは限定的。
}

func (s *fileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// OpenRoot がルート外へのアクセスを拒否するので、".." や
	// エンコードされたパスでの脱出はここで止まる。
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "."
	}

	f, err := s.root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if info.IsDir() {
		// ディレクトリ一覧は出さない。末尾スラッシュへ寄せて index.html だけを配る。
		if !strings.HasSuffix(r.URL.Path, "/") {
			target := r.URL.Path + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		index, err := s.root.Open(path.Join(name, "index.html"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer index.Close()
		indexInfo, err := index.Stat()
		if err != nil || !indexInfo.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		s.serve(w, r, path.Join(name, "index.html"), indexInfo, index)
		return
	}

	if !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	s.serve(w, r, name, info, f)
}

func (s *fileServer) serve(w http.ResponseWriter, r *http.Request, name string, info os.FileInfo, content io.ReadSeeker) {
	ct := contentType(name)
	w.Header().Set("Content-Type", ct)
	// HTML は毎回取りに来させる。旧 nginx の no-store 設定と同じで、
	// デプロイの反映と Anubis のチェッカー通過をキャッシュに邪魔させない。
	if strings.HasPrefix(ct, "text/html") {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(w, r, name, info.ModTime(), content)
}

// statusRecorder はアクセスログ用にステータスコードを記録する。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d", r.Method, r.URL.Path, rec.status)
	})
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	rootDir := os.Getenv("ROOT")
	if rootDir == "" {
		rootDir = "/srv"
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		log.Fatalf("open root %s: %v", rootDir, err)
	}
	defer root.Close()

	srv := &http.Server{
		Addr:    addr,
		Handler: withLog(&fileServer{root: root}),
		// Slowloris 対策に読み取り側へ上限を付ける。配信は大きなファイルが
		// あるので書き込み側のタイムアウトは付けない。
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 14,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on %s, serving %s", addr, rootDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}
