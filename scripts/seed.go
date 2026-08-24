// 造数据脚本：向指定问卷批量提交随机答卷，用于统计正确性验证。
// 用法：
//   go run ./scripts/seed.go -base http://localhost:8080 -survey 3 -n 100
// 说明：答卷按固定随机分布生成，结束后打印每个选项的提交计数，
//       与统计页/导出 CSV 数字核对应一致。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"
)

type question struct {
	ID     int64  `json:"id"`
	Type   string `json:"type"`
	Config struct {
		Options []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"options"`
		Max int `json:"max"`
	} `json:"config"`
	Required bool `json:"required"`
}

func main() {
	base := flag.String("base", "http://localhost:43210", "服务地址")
	surveyID := flag.Int64("survey", 0, "问卷 id（必填）")
	n := flag.Int("n", 100, "提交份数")
	seed := flag.Int64("seed", 42, "随机种子（固定可复现）")
	interval := flag.Float64("interval", 6.2, "每份提交间隔秒数（低于服务限频每分钟 10 份时会被 429 拒绝）")
	flag.Parse()
	if *surveyID == 0 {
		flag.Usage()
		os.Exit(1)
	}
	rng := rand.New(rand.NewSource(*seed))

	// 拉取公开问卷
	resp, err := http.Get(fmt.Sprintf("%s/api/surveys/%d/public", *base, *surveyID))
	must(err)
	defer resp.Body.Close()
	var pub struct {
		Code int `json:"code"`
		Data struct {
			Questions []question `json:"questions"`
		} `json:"data"`
	}
	must(json.NewDecoder(resp.Body).Decode(&pub))
	if pub.Code != 0 {
		fmt.Println("获取问卷失败，code =", pub.Code)
		os.Exit(1)
	}

	// 固定分布计数器
	optionCount := map[string]int{}
	textCount := 0
	okCount := 0

	client := &http.Client{Timeout: 15 * time.Second}
	for i := 0; i < *n; i++ {
		if i > 0 {
			time.Sleep(time.Duration(*interval * float64(time.Second)))
		}
		// 本份的计数先记到 trial，提交成功才并入总数，保证与统计页一致
		trial := map[string]int{}
		trialText := 0
		answers := []map[string]any{}
		for _, q := range pub.Data.Questions {
			switch q.Type {
			case "single_choice", "dropdown":
				o := q.Config.Options[rng.Intn(len(q.Config.Options))]
				answers = append(answers, map[string]any{"question_id": q.ID, "value": o.ID})
				trial[fmt.Sprintf("%d/%s", q.ID, o.ID)]++
			case "multiple_choice":
				k := 1 + rng.Intn(len(q.Config.Options))
				picked := map[string]bool{}
				ids := []string{}
				for j := 0; j < k; j++ {
					o := q.Config.Options[rng.Intn(len(q.Config.Options))]
					if !picked[o.ID] {
						picked[o.ID] = true
						ids = append(ids, o.ID)
						trial[fmt.Sprintf("%d/%s", q.ID, o.ID)]++
					}
				}
				answers = append(answers, map[string]any{"question_id": q.ID, "value": ids})
			case "rating":
				max := q.Config.Max
				if max == 0 {
					max = 5
				}
				v := 1 + rng.Intn(max)
				answers = append(answers, map[string]any{"question_id": q.ID, "value": v})
				trial[fmt.Sprintf("%d/rating", q.ID)]++
			case "text", "textarea":
				if rng.Intn(3) == 0 {
					continue // 部分跳过（非必答）
				}
				answers = append(answers, map[string]any{
					"question_id": q.ID, "value": fmt.Sprintf("第 %d 份答卷的文本建议", i+1)})
				trialText++
			}
		}
		body, _ := json.Marshal(map[string]any{"answers": answers, "duration": 30 + rng.Intn(120)})
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/surveys/%d/responses", *base, *surveyID),
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		must(err)
		var out struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if out.Code == 0 {
			okCount++
			for k, v := range trial {
				optionCount[k] += v
			}
			textCount += trialText
		} else {
			fmt.Printf("第 %d 份失败: code=%d %s\n", i+1, out.Code, out.Msg)
		}
	}

	fmt.Printf("计划提交 %d 份，成功 %d 份\n", *n, okCount)
	fmt.Println("文本题作答数：", textCount)
	fmt.Println("各选项/评分提交计数（与统计页核对）：")
	keys := make([]string, 0, len(optionCount))
	for k := range optionCount {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		fmt.Printf("  %s = %d\n", k, optionCount[k])
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func must(err error) {
	if err != nil {
		fmt.Println("错误:", err)
		os.Exit(1)
	}
}
