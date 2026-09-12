package integration

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"blog-go/internal/chat"
	"github.com/gorilla/websocket"
)

func readFrame(t *testing.T, c *websocket.Conn, typ int) chat.Frame {
	t.Helper()
	for i := 0; i < 100; i++ {
		must(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
		var frame chat.Frame
		must(t, c.ReadJSON(&frame))
		if frame.Type == typ {
			return frame
		}
	}
	t.Fatalf("missing frame %d", typ)
	return chat.Frame{}
}
func socket(t *testing.T, f *fixture, client *http.Client) (*websocket.Conn, chat.Frame) {
	t.Helper()
	home := object(t, f.ok(t, client, "GET", "/", nil))
	endpoint := object(t, home["websiteConfig"])["websocketUrl"].(string)
	headers := http.Header{"Origin": {"http://localhost:8080"}}
	request, err := http.NewRequest("GET", f.cfg.PublicURL, nil)
	must(t, err)
	for _, cookie := range client.Jar.Cookies(request.URL) {
		headers.Add("Cookie", cookie.String())
	}
	c, response, err := websocket.DefaultDialer.Dial(endpoint, headers)
	must(t, err)
	response.Body.Close()
	t.Cleanup(func() { c.Close() })
	history := readFrame(t, c, 2)
	readFrame(t, c, 1)
	return c, history
}
func TestTwoClients(t *testing.T) {
	f := newFixture(t)
	old := time.Now().In(f.cfg.TimeZone).Add(-13 * time.Hour).Format("2006-01-02T15:04:05")
	execSQL(t, f.app.DB, "INSERT INTO tb_chat_record(nickname,avatar,content,ip_address,ip_source,type,create_time,owner_key) VALUES('Old','/old.png','old','127.0.0.1','',3,?,'g:old')", old)
	a, history := socket(t, f, f.guest)
	if len(list(t, object(t, history.Data)["chatRecordList"])) != 0 {
		t.Fatal("expired history was included")
	}
	b, _ := socket(t, f, f.user)
	if readFrame(t, a, 1).Data != float64(2) {
		t.Fatal("two real connections not counted")
	}
	must(t, a.WriteJSON(chat.Frame{Type: 3, Data: map[string]any{"nickname": "Guest", "avatar": f.cfg.PublicURL + "/files/defaults/avatar.svg", "content": "<b>Two-client text</b>", "userId": 999, "ipAddress": "forged"}}))
	first, second := object(t, readFrame(t, a, 3).Data), object(t, readFrame(t, b, 3).Data)
	if first["id"] != second["id"] || first["userId"] != nil || first["ipAddress"] != "127.0.0.1" || strings.Contains(first["content"].(string), "script") {
		t.Fatal("text or trusted ownership did not match on both clients")
	}
	voice := f.upload(t, f.guest, "/voice", "voice.wav", "audio/wav", wavData(), map[string]string{"type": "5", "nickname": "Guest", "avatar": f.cfg.PublicURL + "/files/defaults/avatar.svg"})
	if !voice.result.Flag || voice.result.Data != nil {
		t.Fatal("voice HTTP contract failed")
	}
	v1, v2 := object(t, readFrame(t, a, 5).Data), object(t, readFrame(t, b, 5).Data)
	if v1["id"] != v2["id"] {
		t.Fatal("clients received different voice records")
	}
	response, err := f.guest.Get(v1["content"].(string))
	must(t, err)
	payload, err := io.ReadAll(response.Body)
	response.Body.Close()
	must(t, err)
	if !bytes.Equal(payload, wavData()) {
		t.Fatal("broadcast WAV URL is not readable")
	}
	must(t, a.WriteJSON(chat.Frame{Type: 4, Data: map[string]any{"id": v1["id"], "isVoice": false}}))
	if object(t, readFrame(t, a, 4).Data)["isVoice"] != true {
		t.Fatal("voice recall trusted supplied type")
	}
	readFrame(t, b, 4)
	must(t, b.WriteJSON(chat.Frame{Type: 4, Data: map[string]any{"id": first["id"]}}))
	_, _, err = b.ReadMessage()
	if !websocket.IsCloseError(err, 1008) {
		t.Fatal("same-IP foreign recall was not rejected")
	}
	if readFrame(t, a, 1).Data != float64(1) {
		t.Fatal("disconnection count not broadcast")
	}
	_, history = socket(t, f, newClient(t))
	records := list(t, object(t, history.Data)["chatRecordList"])
	if len(records) != 1 || object(t, records[0])["id"] != first["id"] {
		t.Fatal("reconnect lost text or retained recalled/expired history")
	}
	if rowID(t, f.app.DB, "SELECT count(*) FROM tb_chat_record") != 2 {
		t.Fatal("history filtering deleted durable old facts")
	}
}
