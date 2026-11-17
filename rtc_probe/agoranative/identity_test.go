package agoranative

import "testing"

func TestAudioIdentityRejectsOldChannelWrongPublisherAndUnjoinedConnection(t *testing.T) {
	cases := []struct {
		name, channel, uid string
		connected, want    bool
	}{
		{"current", "current", "10001", true, true},
		{"old channel", "previous", "10001", true, false},
		{"other publisher", "current", "10002", true, false},
		{"empty channel uses bound connection", "", "10001", true, true},
		{"empty channel still rejects other UID", "", "10002", true, false},
		{"unjoined", "current", "10001", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sameAudioSource("current", "10001", c.channel, c.uid, c.connected); got != c.want {
				t.Fatal("callback identity incorrectly accepted", got)
			}
		})
	}
}
