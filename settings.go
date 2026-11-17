package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	myui "github.com/wuyangwang/my-ui"
	"github.com/wuyangwang/my-ui/icons"

	"token-x/core"
)

// settingsPage edits the price table and shows the data locations.
func (a *app) settingsPage(c *ui.Context, t *ui.Theme) {
	a.syncPriceRows() // keep the draft in sync with the table and sessions

	ui.Column(c).FillWidth().Gap(t.Space(4)).Children(func() {
		myui.NewCardPanel(c).Children(func() {
			ui.Text(c, "价格表").FontSize(myui.TextSize(t, myui.TextLg)).Bold()
			ui.Text(c, "USD / 百万 token。codex 的记录不含成本，金额按此表估算；"+
				"走中转站时，把单价改成中转站的实际价格，金额即为真实开销。").
				FontSize(myui.TextSize(t, myui.TextSm)).TextColor(t.TextMuted)

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
				ui.Text(c, "模型").Width(t.Space(56)).Bold()
				ui.Text(c, "Input").Width(t.Space(28)).Bold()
				ui.Text(c, "Cached").Width(t.Space(28)).Bold()
				ui.Text(c, "Output").Width(t.Space(28)).Bold()
			})
			dirty := false
			for _, name := range a.priceOrder {
				row := a.priceDraft[name]
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
					label := name
					if !row.Known {
						label += "（估算）"
					}
					ui.Text(c, label).Width(t.Space(56)).SingleLine()
					myui.NewInput(c.Key("price-in-"+name), &row.Input).Outline().Sm().Width(t.Space(28)).Label(name + " input 单价")
					myui.NewInput(c.Key("price-cached-"+name), &row.Cached).Outline().Sm().Width(t.Space(28)).Label(name + " cached 单价")
					myui.NewInput(c.Key("price-out-"+name), &row.Output).Outline().Sm().Width(t.Space(28)).Label(name + " output 单价")
				})
				dirty = dirty || row.dirty()
			}
			a.priceDirty = dirty
		})

		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			save := myui.NewButton(c.Key("save prices"), "保存价格表").Primary()
			save.Disabled(!a.priceDirty)
			if save.Clicked() {
				a.savePrices()
			}
			if myui.NewButton(c.Key("reset prices"), "恢复默认").Ghost().Clicked() {
				a.resetPrices()
			}
			ui.Box(c).Grow(1)
			ui.Text(c, "价格表保存在 ~/.token-x/prices.json").
				FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
		})

		myui.NewCardPanel(c).Children(func() {
			ui.Text(c, "数据源").FontSize(myui.TextSize(t, myui.TextLg)).Bold()
			ui.Row(c).FillWidth().Gap(t.Space(6)).AlignItems(ui.Center).Children(func() {
				for _, s := range a.reg.Sources {
					live := s.Available() == nil
					state, color := "不可用", t.TextMuted
					if live {
						state, color = "已连接", t.Success
					}
					ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icons.Circle()).Size(t.Space(3), t.Space(3)).TextColor(color)
						ui.Text(c, s.Name()).Bold()
						ui.Text(c, state).FontSize(myui.TextSize(t, myui.TextSm)).TextColor(t.TextMuted)
					})
				}
			})
			ui.Text(c, "codex 读取 ~\\.codex（可用 CODEX_HOME 覆盖）；opencode 只读 "+
				"~\\.local\\share\\opencode\\opencode.db，删除走官方 CLI。").
				FontSize(myui.TextSize(t, myui.TextSm)).TextColor(t.TextMuted)
		})
	})
}

// syncPriceRows builds the editable draft from the price table plus every
// model seen in the loaded sessions. The draft's rows are stable pointers:
// the inputs edit them in place.
func (a *app) syncPriceRows() {
	if a.priceDraft == nil {
		a.priceDraft = map[string]*priceRow{}
	}
	seen := map[string]bool{}

	put := func(model string, p core.ModelPrice, known bool) {
		if seen[model] {
			return
		}
		seen[model] = true
		if row, ok := a.priceDraft[model]; ok {
			row.Known = known
			return
		}
		a.priceDraft[model] = &priceRow{
			Model:  model,
			Input:  fmtNum(p.Input),
			Cached: fmtNum(p.CachedInput),
			Output: fmtNum(p.Output),
			Known:  known,
		}
	}

	for m, p := range a.prices.Models {
		put(m, p, true)
	}
	for _, s := range a.session {
		if _, ok := a.prices.Models[s.Model]; !ok {
			p, _ := a.prices.PriceOf(s.Model)
			put(s.Model, p, false)
		}
	}

	// Known models first, then by name.
	names := make([]string, 0, len(a.priceDraft))
	for name := range a.priceDraft {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := a.priceDraft[names[i]], a.priceDraft[names[j]]
		if a.Known != b.Known {
			return a.Known
		}
		return names[i] < names[j]
	})
	a.priceOrder = names
}

// dirty reports whether any field text is not a valid number.
func (r *priceRow) dirty() bool {
	for _, s := range []string{r.Input, r.Cached, r.Output} {
		if s == "" {
			return true
		}
		if _, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err != nil {
			return true
		}
	}
	return false
}

func (a *app) savePrices() {
	models := map[string]core.ModelPrice{}
	for name, r := range a.priceDraft {
		in, _ := strconv.ParseFloat(strings.TrimSpace(r.Input), 64)
		ca, _ := strconv.ParseFloat(strings.TrimSpace(r.Cached), 64)
		out, _ := strconv.ParseFloat(strings.TrimSpace(r.Output), 64)
		models[name] = core.ModelPrice{Input: in, CachedInput: ca, Output: out}
	}
	a.prices.Models = models
	if err := a.prices.Save(); err != nil {
		a.toast = pendingToast{Title: "保存失败", Desc: err.Error(), Error: true}
	} else {
		a.toast = pendingToast{Title: "价格表已保存", Desc: "金额按新单价计算，刷新后生效"}
		a.reload()
	}
	a.priceDirty = false
}

func (a *app) resetPrices() {
	a.prices.Models = core.DefaultPrices().Models
	a.priceDraft = nil
	a.priceOrder = nil
	a.priceDirty = false
	a.toast = pendingToast{Title: "已恢复默认价格", Desc: "记得点“保存价格表”写入磁盘"}
}

func fmtNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
