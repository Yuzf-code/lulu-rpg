package llm

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Mock 是内置演示写手：按行标签协议输出脚本化的剧情片段，
// 让整个应用在没有真实模型时也能完整体验（含流式效果）。
type Mock struct {
	Delay time.Duration // 每个 chunk 之间的模拟延迟
}

// NewMock 创建演示写手。
func NewMock() *Mock { return &Mock{Delay: 25 * time.Millisecond} }

func (m *Mock) Stream(ctx context.Context, req Request, onChunk func(string) error) (string, error) {
	text := m.compose(req)
	for _, r := range text {
		if err := ctx.Err(); err != nil {
			return "", ctx.Err()
		}
		if onChunk != nil {
			if err := onChunk(string(r)); err != nil {
				return "", err
			}
		}
		time.Sleep(m.Delay)
	}
	return text, nil
}

func (m *Mock) Complete(ctx context.Context, req Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return m.compose(req), nil
}

func (m *Mock) compose(req Request) string {
	// 辅助任务（非剧情生成）各有固定的演示输出。
	switch req.Mock.Task {
	case TaskSummary:
		return "主角一行在灰隼客栈集结，约定前往雾谷调查废弃钟楼；夜里山道中听到不应存在的钟声；" +
			"黎明抵达塔前，发现塔门有修士使用过的新鲜火堆余烬，门闩也是新的——有人在等他们。\n"
	case TaskGreeting:
		return "（她抬起头，目光在炉火里晃了晃，上下打量着你）……路远吗？不急，先坐下喝口热的。" +
			"故事总要有个开头——不如，就从你的来处说起。"
	case TaskDialogues:
		return `[{"user":"你为什么留在这里？","char":"总得有人守着这盏灯。灭了，山里的人就找不到回来的路。"},{"user":"这些情报什么价？","char":"不收钱。讲一个我没听过的真事，就算两清。"},{"user":"前面那段路安全吗？","char":"白天安全。夜里……如果你不想知道夜里有什么，就别在夜里走。"}]`
	case TaskDetect:
		return `["无名旅人","雾中修士"]`
	case TaskStyle:
		return `{"name":"冷峻悬疑","description":"[旁白]以环境白描与感官细节（寒气、声响、气味）营造压迫感，短句为主、少形容词堆砌；台词简短藏机锋，避开寒暄；[动作]写克制的小动作（叩剑柄、收手指）而非大开大合；[内心]用短促的自我诘问；整体情绪藏在留白里，不直陈。"}`
	case TaskDistill:
		return m.distillCard(req.Mock.Target, req.Mock.Subject)
	case TaskInspiration:
		return m.inspiration(req.Mock.PersonaName, req.Mock.CharNames)
	}
	names := req.Mock.CharNames
	if len(names) == 0 {
		names = []string{"旅人"}
	}
	name := func(i int) string {
		if len(names) == 0 {
			return "旅人"
		}
		return names[i%len(names)]
	}
	var lines []string
	if req.Mock.Opening {
		lines = m.opening(name, req.Mock.PersonaName)
	} else {
		lines = m.continue_(name, int(req.Mock.Turn))
	}
	return strings.Join(lines, "\n") + "\n"
}

// distillCard 返回一张演示蒸馏卡；目标/主体名取自请求提示。
func (m *Mock) distillCard(target, subject string) string {
	if target == "" {
		target = "无名旅人"
	}
	rel := ""
	if subject != "" {
		rel = `,"relationships":[{"subject":` + strconvJSON(subject) +
			`,"text":"（示例蒸馏）保持距离的观察，却在关键处暗中相助；旧账未清，恩怨未明。"}]`
	}
	return `{"name":` + strconvJSON(target) +
		`,"title":"雾谷的守望者"` +
		`,"appearance":"风尘仆仆的旅装，眉眼间带着旧伤的影子，随身一盏旧灯笼"` +
		`,"personality":"谨慎寡言，恩怨分明；话不多，但每个字都有分量"` +
		`,"background":"关于` + target + `的记载散见于旅人手记：三年前钟楼坍塌之夜出现在雾谷，此后便留在山道旁，为迷路者引路，从不谈自己的来历。"` +
		`,"greeting":"（提起灯笼照了照你脚下的路）这条道夜间不好走。跟我来，还是原路回去——现在就得选。"` +
		`,"tags":["蒸馏示例","守望者"]` +
		`,"example_dialogues":[{"user":"你为什么守在这里？","char":"总得有人守着这盏灯。"},{"user":"前面安全吗？","char":"白天安全。夜里的事，别在白天问。"}]` +
		rel +
		`}`
}

// inspiration 返回若干条演示灵感（台词与导演指令混合）。
func (m *Mock) inspiration(persona string, chars []string) string {
	a := "旅伴"
	if len(chars) > 0 {
		a = chars[0]
	}
	return `[{"kind":"say","text":"“这盏灯，三年前那一夜也亮过——对吗？”"},` +
		`{"kind":"say","text":"“` + a + `，你先带路，我来断后。”"},` +
		`{"kind":"direct","text":"钟楼的钟在无人触碰的情况下突然自鸣，惊起满山飞鸟。"},` +
		`{"kind":"direct","text":"一名黑衣修士从塔影中走出，无声地拦住去路。"},` +
		`{"kind":"say","text":"“先退到林线，看清楚再动。”"},` +
		`{"kind":"direct","text":"暴风雨在黎明前降临，一行人被迫进塔躲避。"}]`
}

// strconvJSON 把字符串编码为 JSON 字符串字面量（mock 专用，失败退化为引号包裹）。
func strconvJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `"` + strings.ReplaceAll(s, `"`, ``) + `"`
	}
	return string(b)
}

func (m *Mock) opening(name func(int) string, persona string) []string {
	a, b := name(0), name(1)
	if b == a {
		b = ""
	}
	lines := []string{
		"[旁白]暮色像一匹浸了蜜的绸缎，缓缓覆上「灰隼客栈」的屋脊。壁炉里的火噼啪作响，把人影投在斑驳的墙上。",
		"[旁白]风从门缝钻进来，带着松脂与远方尘土的气息。桌上的油灯轻轻晃了一下。",
		"[" + a + "·动作]" + a + "指尖搭在杯沿，目光却越过杯口，落在推门而入的身影上。",
		"[" + a + "·内心]（这位" + persona + "……周身的尘土掩不住那股江湖气，绝不是普通的旅人。）",
	}
	if b != "" {
		lines = append(lines,
			"["+b+"]"+"别傻站着，"+persona+"。炉边还有空位——在这样的夜里，故事总比面包先端上来。",
		)
	}
	lines = append(lines,
		"["+a+"]"+"随便坐。只是有个规矩：在灰隼客栈，你带来的秘密，得换一个故事才走得掉。",
	)
	return lines
}

// continue_ 按回合号轮换三段不同的脚本，保证演示时有推进感。
func (m *Mock) continue_(name func(int) string, turn int) []string {
	a, b := name(0), name(1)
	if b == a {
		b = ""
	}
	sets := [][]string{
		{
			"[旁白]火光一跳，地图上蜿蜒的红线仿佛活了过来，直指群山深处那座被云雾吞没的钟楼。",
			"[" + a + "·动作]" + a + "把一枚铜币按在地图中央，指节因用力而微微发白。",
			"[" + a + "·内心]（钟楼的钥匙早就碎了……可昨天夜里，确实有人在塔顶点起了灯。）",
			"[" + a + "]三年了。所有人都说那条路通向坟场——但灯又亮了，总得有人去看看。",
			"[" + b + "]那就别在客栈里空谈。（" + b + "把斗篷甩上肩头，扣好最后一枚搭扣。）天亮前出发，还能赶上夜露凝结的时刻。",
		},
		{
			"[旁白]后半夜的山路湿滑难行。雾气贴着地面流淌，火把的光只能推开三步之内的一小团昏黄。",
			"[" + a + "·动作]" + a + "忽然抬手止住众人，侧耳贴向风声。",
			"[" + a + "·内心]（不对……这雾里有铃声。死了三年的钟，不会自己响。）",
			"[" + b + "]是守夜人的铃。传说钟楼塌了之后，只有他们还在废墟里巡夜……据说，他们不喜欢活人的脚步声。",
			"[" + a + "]那就把脚步放轻。" + "（" + a + "回头看了你一眼）" + "——你走中间，别碰任何反光的东西。",
		},
		{
			"[旁白]破晓时分，钟楼的轮廓终于从雾里浮现。塔身斜斜地刺向天空，像一柄折断的剑。",
			"[旁白]塔门前散落着新鲜的火堆余烬——有人昨晚确实来过。",
			"[" + a + "·动作]" + a + "蹲下身，捻起一点灰烬，放在鼻尖轻嗅。",
			"[" + a + "]松脂，还有一点点圣盐……修士的火。可这座塔塌的时候，修道院就已经空了。",
			"[" + b + "·内心]（如果修士回来了，那我们昨晚在山道上听到的铃声，恐怕就不是什么守夜人……）",
			"[" + b + "]门闩是新的。要推门吗，还是……先听听里面在等谁？",
		},
	}
	return sets[turn%len(sets)]
}
