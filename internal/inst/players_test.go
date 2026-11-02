package inst

import (
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"minedeck/internal/events"
)

// 回归：旧的进服、退服正则没有锚定，玩家在聊天里打一句「Notch joined the game」就会把 Notch 加进在线列表、
// 推送 player.join；打「Alex left the game」能把 Alex 删掉。现在只认锚定在正文上的系统消息，
// 一次进服的「logged in」和「joined the game」两行也只推送一次事件。
func TestTrackPlayersIgnoresForgedLines(t *testing.T) {
	const name = "track-test"
	var mu sync.Mutex
	var got []string
	events.Subscribe(func(e events.Event) {
		if e.Data["instance"] == name {
			mu.Lock()
			got = append(got, e.Type+" "+e.Data["player"].(string))
			mu.Unlock()
		}
	})
	i := &Instance{Settings: Settings{Name: name}}
	online := func() []string { return slices.Sorted(maps.Keys(i.online)) }
	feed := func(lines ...string) {
		for _, l := range lines {
			i.trackPlayers(l)
		}
	}
	const p = "[20:15:20] [Server thread/INFO] [minecraft/MinecraftServer]: "
	feed(
		"[20:15:20] [Server thread/INFO] [minecraft/PlayerList]: Steve[/127.0.0.1:51234] logged in with entity id 112 at (8.5, 64.0, 8.5)",
		p+"Steve joined the game",
		p+"Alex joined the game",
		p+"[Not Secure] <Steve> Notch joined the game",
		p+"[Not Secure] <Steve> Alex left the game",
		p+"[Not Secure] * Steve Notch joined the game",
		p+"[Server] Alex left the game",
		"> say Notch joined the game",
		"[20:15:40] [Server thread/INFO] [examplemod/]: Notch joined the game",
	)
	if o := online(); !slices.Equal(o, []string{"Alex", "Steve"}) {
		t.Fatalf("伪造的进服 / 退服生效了，在线：%v", o)
	}
	feed(
		"[20:16:30] [Server thread/INFO] [minecraft/ServerGamePacketListenerImpl]: Steve lost connection: Disconnected",
		p+"Steve left the game",
	)
	if o := online(); !slices.Equal(o, []string{"Alex"}) {
		t.Fatalf("Steve 退服后在线应只剩 Alex：%v", o)
	}
	want := []string{"player.join Steve", "player.join Alex", "player.leave Steve"}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		g := slices.Clone(got)
		mu.Unlock()
		if slices.Equal(g, want) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("推送的事件：%v，应为 %v", g, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
