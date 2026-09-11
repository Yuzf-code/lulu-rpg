package store

import (
	"encoding/json"
	"log"
)

// SeedDemoData 在数据库完全为空时播种示例角色与玩家档案，
// 让演示模式开箱即玩；真实使用中用户可直接删除或修改。
func SeedDemoData(s *Store) error {
	cs, err := s.ListCharacters()
	if err != nil {
		return err
	}
	ps, err := s.ListPersonas()
	if err != nil {
		return err
	}
	if len(cs) > 0 || len(ps) > 0 {
		return nil
	}

	exA, _ := json.Marshal([]ExampleDialogue{
		{User: "你相信钟楼的传说吗？", Char: "信，也不信。传说是死人的回声——可昨天夜里，那口钟确实响了。"},
	})
	relA, _ := json.Marshal([]Relationship{
		{Subject: "林远", Text: "初见即觉眼熟，戒备之中藏着一丝说不清的在意——那本旧笔记让她想起某位故人。"},
	})
	exB, _ := json.Marshal([]ExampleDialogue{
		{User: "这酒多少钱？", Char: "钱？哈，故事比钱值钱。讲一段你没讲过的，这杯算我请。"},
	})
	tagsA, _ := json.Marshal([]string{"佣兵", "剑士"})
	tagsB, _ := json.Marshal([]string{"酒馆老板", "情报贩子"})

	demo := []*Character{
		{Name: "艾莉娅·风语", Title: "流浪剑士", Tags: tagsA, ExampleDialogues: exA, Relationships: relA,
			Appearance:  "银灰色短发，左眉有一道旧疤；深绿色旅行斗篷，腰间一柄旧剑，剑柄缠着褪色的红绳。",
			Personality: "外冷内热，话少但句句见血；警惕心极重，却会为陌生人拔剑；讨厌被人道谢。",
			Background:  "曾是王国边境巡逻队的百夫长，三年前“钟楼之夜”后小队全灭，只她一人生还。她一直在追查那晚的真相，线索指向雾谷深处的废弃钟楼。",
			Greeting:    "（她把剑横在膝上，用布慢慢擦拭，头也不抬。）……坐可以，别碰我的剑。还有——如果你也要去雾谷，最好先想清楚，你自己有几条命。"},
		{Name: "老巴德", Title: "灰隼客栈老板", Tags: tagsB, ExampleDialogues: exB,
			Appearance:  "圆胖身材，红鼻头，围着油腻的白围裙，笑起来眼睛眯成两条缝；围裙口袋里总插着一把擦得锃亮的开瓶器。",
			Personality: "八面玲珑，嘴上没正经，肚里有一本活地图；收钱办事，但认死理：客栈里不动刀兵。",
			Background:  "年轻时跑遍大陆的商队护卫，退隐后开了灰隼客栈。各路消息在他这里汇成河流——只要你付得起价钱，或者讲一个够好的故事。"},
	}
	for _, c := range demo {
		if err := s.CreateCharacter(c); err != nil {
			return err
		}
	}
	p := &Persona{Name: "林远", IsDefault: true,
		Description: "来历不明的年轻旅人，随身带一本写满奇怪符号的旧笔记；似乎在寻找某样东西，被人问起时总以笑带过。"}
	if err := s.CreatePersona(p); err != nil {
		return err
	}
	log.Printf("[seed] 已播种示例角色与档案（可在应用内删除）")
	return nil
}
