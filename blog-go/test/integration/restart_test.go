package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"blog-go/internal/platform"
)

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	must(t, err)
	must(t, listener.Close())
	return port
}
func cloneBackend(t *testing.T) string {
	t.Helper()
	source := projectRoot(t)
	target := filepath.Join(t.TempDir(), "backend")
	for _, name := range []string{"go.mod", "go.sum", "cmd", "internal", "scripts"} {
		path := filepath.Join(source, name)
		must(t, filepath.Walk(path, func(file string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasSuffix(file, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(source, file)
			if err != nil {
				return err
			}
			destination := filepath.Join(target, relative)
			if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				return err
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			return os.WriteFile(destination, data, info.Mode().Perm())
		}))
	}
	return target
}
func scriptEnv(data, port string) []string {
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(key, "BLOG_") {
			env = append(env, value)
		}
	}
	return append(env, "BLOG_DATA_DIR="+data, "BLOG_ADDR=127.0.0.1:"+port, "BLOG_PUBLIC_URL=http://localhost:"+port, "BLOG_FRONTEND_URL=http://localhost:8080", "BLOG_ALLOWED_ORIGINS=http://localhost:8080,http://localhost:8081", "BLOG_UPLOAD_MODE=local", "BLOG_IP_ENABLED=false")
}
func runScript(t *testing.T, root, name string, env []string) string {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(root, "scripts", name))
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, out)
	}
	return string(out)
}

func TestPersistentRestart(t *testing.T) {
	root := cloneBackend(t)
	data := filepath.Join(t.TempDir(), "state")
	port := freePort(t)
	env := scriptEnv(data, port)
	first := runScript(t, root, "start.sh", env)
	if !strings.Contains(first, "Backend PID:") {
		t.Fatal("start did not report owned process")
	}
	t.Cleanup(func() {
		cmd := exec.Command("bash", filepath.Join(root, "scripts/stop.sh"))
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("cleanup stop: %v %s", err, out)
		}
	})
	duplicate := exec.Command("bash", filepath.Join(root, "scripts/start.sh"))
	duplicate.Env = env
	if _, err := duplicate.CombinedOutput(); err == nil {
		t.Fatal("second start was accepted")
	}
	credential, err := os.ReadFile(filepath.Join(data, "initial-admin.txt"))
	must(t, err)
	values := map[string]string{}
	for _, line := range strings.Split(string(credential), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	f := &fixture{cfg: platform.Config{PublicURL: "http://localhost:" + port}, hits: make(map[string]int), admin: newClient(t), guest: newClient(t)}
	f.ok(t, f.admin, "POST", "/login", url.Values{"username": {values["username"]}, "password": {values["password"]}})
	f.ok(t, f.admin, "PUT", "/admin/about", map[string]any{"aboutContent": "# Persistent restart proof"})
	r := f.upload(t, f.admin, "/admin/articles/images", "persist.png", "image/png", imageData(t, false), nil)
	if !r.result.Flag {
		t.Fatal("restart image upload failed")
	}
	imageURL := r.result.Data.(string)
	body := "# Restart body\n![persisted](" + imageURL + ")"
	f.ok(t, f.admin, "POST", "/admin/articles", map[string]any{"articleTitle": "Restart article", "articleContent": body, "articleCover": imageURL, "categoryName": "Restart category", "tagNameList": []string{"Restart tag"}, "type": 1, "status": 1})
	articles := page(t, f.ok(t, f.admin, "GET", "/admin/articles?keywords=Restart", nil))
	record := object(t, list(t, articles["recordList"])[0])
	id := int64(record["id"].(float64))
	before := object(t, f.ok(t, f.guest, "GET", pathID("/articles/%d", id), nil))
	runScript(t, root, "stop.sh", env)
	if _, err := os.Stat(filepath.Join(data, "blog.db")); err != nil {
		t.Fatal("stop removed database")
	}
	runScript(t, root, "start.sh", env)
	f.fail(t, f.admin, "GET", "/admin/user/menus", nil, platform.CodeFail)
	f.ok(t, f.admin, "POST", "/login", url.Values{"username": {values["username"]}, "password": {values["password"]}})
	if f.ok(t, f.guest, "GET", "/about", nil) != "# Persistent restart proof" {
		t.Fatal("about lost after process restart")
	}
	after := object(t, f.ok(t, f.guest, "GET", pathID("/articles/%d", id), nil))
	if after["articleContent"] != body || after["articleCover"] != imageURL || after["viewsCount"] != before["viewsCount"] {
		t.Fatal("article, file reference or view fact lost on restart")
	}
	assertImage(t, f.guest, imageURL, "image/png")
	current, err := os.ReadFile(filepath.Join(data, "initial-admin.txt"))
	must(t, err)
	if !bytes.Equal(credential, current) {
		t.Fatal("restart reset initial credentials")
	}
	runScript(t, root, "stop.sh", env)
}

func TestStopRefusesPIDForDifferentExecutable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "isolated")
	script, err := os.ReadFile(filepath.Join(projectRoot(t), "scripts/stop.sh"))
	must(t, err)
	write(t, filepath.Join(root, "scripts/stop.sh"), string(script))
	must(t, os.MkdirAll(filepath.Join(root, "bin"), 0755))
	process := exec.Command("/bin/sleep", "30")
	must(t, process.Start())
	defer func() { process.Process.Kill(); process.Wait() }()
	started, err := exec.Command("ps", "-p", fmt.Sprint(process.Process.Pid), "-o", "lstart=").Output()
	must(t, err)
	metadata, _ := json.Marshal(map[string]any{"pid": process.Process.Pid, "executable": filepath.Join(root, "bin/blog-server"), "started": strings.TrimSpace(string(started))})
	write(t, filepath.Join(root, "bin/blog.pid"), string(metadata))
	cmd := exec.Command("bash", filepath.Join(root, "scripts/stop.sh"))
	if _, err := cmd.CombinedOutput(); err == nil {
		t.Fatal("foreign executable was accepted")
	}
	must(t, process.Process.Signal(syscall.Signal(0)))
	if _, err := os.Stat(filepath.Join(root, "bin/blog.pid")); err != nil {
		t.Fatal("refused PID evidence was discarded")
	}
}

func TestStartRefusesAnAlreadyListeningPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "foreign listener") }))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "isolated")
	script, err := os.ReadFile(filepath.Join(projectRoot(t), "scripts/start.sh"))
	must(t, err)
	write(t, filepath.Join(root, "scripts/start.sh"), string(script))
	address, err := url.Parse(server.URL)
	must(t, err)
	cmd := exec.Command("bash", filepath.Join(root, "scripts/start.sh"))
	cmd.Env = scriptEnv(t.TempDir(), address.Port())
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "Address already in use") {
		t.Fatalf("occupied listener was not rejected before building: %v %s", err, output)
	}
	response, err := http.Get(server.URL)
	must(t, err)
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	must(t, err)
	if string(body) != "foreign listener" {
		t.Fatal("start affected existing listener")
	}
}

func TestFrontendPreviewPreservesAPIAndSPAContracts(t *testing.T) {
	type requestFact struct {
		method, path, cookie string
		body                 []byte
	}
	requests := make(chan requestFact, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		requests <- requestFact{r.Method, r.RequestURI, r.Header.Get("Cookie"), body}
		w.Header().Add("Set-Cookie", "first=one; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", "second=two; Path=/")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(207)
		w.Write(body)
	}))
	defer upstream.Close()
	output := t.TempDir()
	write(t, filepath.Join(output, "blog/dist/index.html"), "<html>Original SPA</html>")
	write(t, filepath.Join(output, "blog/dist/app.js"), "window.original=true;")
	port := freePort(t)
	log, err := os.Create(filepath.Join(t.TempDir(), "preview.log"))
	must(t, err)
	cmd := exec.Command("python3", filepath.Join(projectRoot(t), "scripts/prepare-frontends.py"), "--serve", "blog", "--output", output, "--port", port, "--backend", upstream.URL)
	cmd.Stdout = log
	cmd.Stderr = log
	must(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			cmd.Process.Kill()
			<-done
		}
		log.Close()
	})
	base := "http://localhost:" + port
	client := &http.Client{Timeout: 3 * time.Second}
	ready := false
	for i := 0; i < 50; i++ {
		response, err := client.Get(base + "/")
		if err == nil {
			response.Body.Close()
			ready = response.StatusCode == 200
			if ready {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("production preview did not start")
	}
	response, err := client.Get(base + "/articles/42")
	must(t, err)
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	must(t, err)
	if string(body) != "<html>Original SPA</html>" {
		t.Fatal("SPA history fallback failed")
	}
	response, err = client.Get(base + "/missing.js")
	must(t, err)
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("missing JS was masked as SPA success")
	}
	payload := []byte("multipart\x00payload 中文")
	request, err := http.NewRequest("POST", base+"/api/echo%2Fraw?q=%E4%B8%AD", bytes.NewReader(payload))
	must(t, err)
	request.Header.Set("Cookie", "session=preserved")
	request.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	response, err = client.Do(request)
	must(t, err)
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	must(t, err)
	fact := <-requests
	if response.StatusCode != 207 || !bytes.Equal(body, payload) || len(response.Header.Values("Set-Cookie")) != 2 || fact.method != "POST" || fact.path != "/echo%2Fraw?q=%E4%B8%AD" || fact.cookie != "session=preserved" || !bytes.Equal(fact.body, payload) {
		t.Fatal("API proxy altered method, query, cookies, status or upload bytes")
	}
}
