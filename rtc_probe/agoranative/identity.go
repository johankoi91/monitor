package agoranative

// The audio callback may omit channelId; its validated connection still binds it
// to one round. A nonempty channel and the publisher UID must always match.
func sameAudioSource(expectedChannel, expectedUID, channel, uid string, connected bool) bool {
	return connected && expectedChannel != "" && expectedUID != "" && (channel == "" || channel == expectedChannel) && uid == expectedUID
}
