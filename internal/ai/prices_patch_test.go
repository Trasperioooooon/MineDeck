package ai

import (
	"fmt"
	"reflect"
	"testing"
)

func usd(model string, in float64) CustomPrice {
	return CustomPrice{Model: model, Currency: "USD", Input: in, Output: 1}
}

// 两个窗口同时开着「设置 → AI」的自填单价：A 先加了 m1 并存下；B 还是 A 改之前的样子（只有 m0），删掉 m0，
// 只发这一条删除。A 加的 m1 留着，B 删的 m0 没了；存进设置文件，重启以后一样。改单价不动服务商设置的 rev（2026-11-17）。
func TestPricesPatchTwoWindows(t *testing.T) {
	dir := t.TempDir()
	s := NewService(Options{Dir: dir, Keys: &memKeys{m: map[string]string{}}, HTTP: (&noDial{}).client()})
	rev := s.View().Provider.Rev
	if err := s.SetPrices([]CustomPrice{usd("m0", 1)}); err != nil {
		t.Fatal(err)
	}
	if err := s.PatchPrices(PricesPatch{Add: []CustomPrice{usd("m1", 2)}}); err != nil { // 窗口 A
		t.Fatal(err)
	}
	if err := s.PatchPrices(PricesPatch{Remove: []string{"m0"}}); err != nil { // 窗口 B
		t.Fatal(err)
	}
	want := []CustomPrice{usd("m1", 2)}
	if got := s.View().CustomPrices; !reflect.DeepEqual(got, want) {
		t.Errorf("应只剩 A 加的 m1：%+v", got)
	}
	if got := NewService(Options{Dir: dir, Keys: &memKeys{m: map[string]string{}}}).View().CustomPrices; !reflect.DeepEqual(got, want) {
		t.Errorf("存进设置文件的：%+v", got)
	}
	if s.View().Provider.Rev != rev {
		t.Error("改自填单价不该改服务商设置的 rev")
	}
}

// 按条加、删的规矩：已经有的模型是原地换掉；删不存在的不算错；先整张替换、再删、再加；
// addPrices 里同一个模型两次、某一条不合格、合起来超过 100 条都报错，整次不改。老写法（整张替换）照旧。
func TestPricesPatchRules(t *testing.T) {
	s := NewService(Options{Dir: t.TempDir(), Keys: &memKeys{m: map[string]string{}}, HTTP: (&noDial{}).client()})
	expect := func(what string, want ...CustomPrice) {
		t.Helper()
		if got := s.View().CustomPrices; !reflect.DeepEqual(got, append([]CustomPrice{}, want...)) {
			t.Errorf("%s：%+v，应为 %+v", what, got, want)
		}
	}
	if err := s.SetPrices([]CustomPrice{usd("m0", 1), usd("m1", 1)}); err != nil {
		t.Fatal(err)
	}
	if err := s.PatchPrices(PricesPatch{Add: []CustomPrice{usd("m0", 9)}}); err != nil {
		t.Fatal(err)
	}
	expect("添加已有的模型是原地换掉", usd("m0", 9), usd("m1", 1))
	if err := s.PatchPrices(PricesPatch{Remove: []string{"m9", "M1", "m1 "}}); err != nil {
		t.Fatalf("删不存在的不算错：%v", err)
	}
	expect("模型名完全一致才删", usd("m0", 9), usd("m1", 1))
	if err := s.PatchPrices(PricesPatch{Prices: &[]CustomPrice{usd("x", 1), usd("y", 1)}, Remove: []string{"x"}, Add: []CustomPrice{usd("z", 1), usd("y", 5)}}); err != nil {
		t.Fatal(err)
	}
	expect("先替换、再删、再加", usd("y", 5), usd("z", 1))

	for _, c := range []struct {
		p    PricesPatch
		want string
	}{
		{PricesPatch{Remove: []string{"y"}, Add: []CustomPrice{usd("a", 1), usd("a", 2)}}, "模型 a 填了两次"},
		{PricesPatch{Remove: []string{"y"}, Add: []CustomPrice{usd("a", 1), {Model: "b", Currency: "EUR"}}}, "b：币种只能是 CNY（元）或 USD（美元）"},
		{PricesPatch{Prices: &[]CustomPrice{usd("c", 1), usd("c", 1)}}, "模型 c 填了两次"},
	} {
		if err := s.PatchPrices(c.p); err == nil || err.Error() != c.want {
			t.Errorf("应报「%s」：%v", c.want, err)
		}
	}
	expect("报错的改动整次不改", usd("y", 5), usd("z", 1))

	// 条数：合起来超过 100 条报错、不改；正好 100 条可以
	var many []CustomPrice
	for k := range 99 {
		many = append(many, usd(fmt.Sprintf("p%d", k), 1))
	}
	if err := s.SetPrices(many); err != nil {
		t.Fatal(err)
	}
	if err := s.PatchPrices(PricesPatch{Add: []CustomPrice{usd("q1", 1), usd("q2", 1)}}); err == nil || err.Error() != "自填单价最多 100 条" {
		t.Errorf("合起来 101 条应报错：%v", err)
	}
	if n := len(s.View().CustomPrices); n != 99 {
		t.Errorf("超出时不该改：%d 条", n)
	}
	if err := s.PatchPrices(PricesPatch{Remove: []string{"p0"}, Add: []CustomPrice{usd("q1", 1), usd("q2", 1)}}); err != nil {
		t.Errorf("先删一条再加两条正好 100 条：%v", err)
	}
	if n := len(s.View().CustomPrices); n != 100 {
		t.Errorf("应是 100 条：%d", n)
	}
	// 老写法：整张替换，101 条报错，空表清空
	if err := s.SetPrices(append(many, usd("q1", 1), usd("q2", 1))); err == nil || err.Error() != "自填单价最多 100 条" {
		t.Errorf("整张替换 101 条：%v", err)
	}
	if err := s.SetPrices(nil); err != nil {
		t.Fatal(err)
	}
	expect("整张替换成空的")
}
