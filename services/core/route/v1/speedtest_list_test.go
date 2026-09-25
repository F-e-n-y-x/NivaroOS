package v1

import "testing"

func TestParseOoklaXML(t *testing.T) {
	b := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<settings><servers>
<server url="http://a.example:8080/speedtest/upload.php" name="Bangalore" sponsor="Sri Lakshmi Networks" host="a.example:8080" />
<server url="http://b.example:8080/speedtest/upload.php" name="Delhi" sponsor="Anonet" host="b.example:8080" />
<server name="No host" sponsor="x" />
</servers></settings>`)
	got := parseOoklaXML(b)
	if len(got) != 2 || got[0] != (ooklaServer{"http", "a.example:8080", "Bangalore", "Sri Lakshmi Networks"}) || got[1].Host != "b.example:8080" {
		t.Fatalf("got %+v", got)
	}
	if parseOoklaXML([]byte("not xml")) != nil {
		t.Fatal("bad XML should give no servers")
	}
	tg := ooklaTarget("http", "a.example:8080", "x")
	if tg.PingURL != "http://a.example:8080/hello" || tg.UpURL != "http://a.example:8080/upload" {
		t.Fatalf("target %+v", tg)
	}
}
