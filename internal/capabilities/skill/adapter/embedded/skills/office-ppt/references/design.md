# PPT 设计原则（对齐 Anthropic pptx）

**不要做无聊的幻灯片。** 白底大段项目符号不够用。创建前先读本节；细节也可对照 Anthropic `pptx` Skill。

## 开始前

- **选与主题绑定的配色**：换到别的主题就「不合适」才算选对。
- **主色主导**：一色占 60–70% 视觉权重，1–2 个辅色 + 一个锐利强调色。
- **明暗对比**：标题/结尾深色、内容浅色（三明治），或全程深色高级感。
- **一个视觉母题贯穿**：圆角图框、色圈图标、单侧粗边等，全稿一致。

## 配色灵感

选与主题匹配的配色——不要默认泛蓝：

| Theme | Primary | Secondary | Accent |
|-------|---------|-----------|--------|
| Midnight Executive | `1E2761` (navy) | `CADCFC` (ice blue) | `FFFFFF` (white) |
| Forest & Moss | `2C5F2D` (forest) | `97BC62` (moss) | `F5F5F5` (cream) |
| Coral Energy | `F96167` (coral) | `F9E795` (gold) | `2F3C7E` (navy) |
| Warm Terracotta | `B85042` (terracotta) | `E7E8D1` (sand) | `A7BEAE` (sage) |
| Ocean Gradient | `065A82` (deep blue) | `1C7293` (teal) | `21295C` (midnight) |
| Charcoal Minimal | `36454F` (charcoal) | `F2F2F2` (off-white) | `212121` (black) |
| Teal Trust | `028090` (teal) | `00A896` (seafoam) | `02C39A` (mint) |
| Berry & Cream | `6D2E46` (berry) | `A26769` (dusty rose) | `ECE2D0` (cream) |
| Sage Calm | `84B59F` (sage) | `69A297` (eucalyptus) | `50808E` (slate) |
| Cherry Bold | `990011` (cherry) | `FCF6F5` (off-white) | `2F3C7E` (navy) |

## 每页

**每页至少一个视觉元素**——图/表/图标/形状。纯文字页容易被忘掉。

**布局可轮换：** 双栏；图标+文本行；2x2 / 2x3 网格；半幅出血图 + 内容叠层。

**数据展示：** 大数字 callout（60–72pt）、对比栏、时间线/流程。

**字体配对（勿默认 Arial 一套到底）：**

| Header | Body |
|--------|------|
| Georgia | Calibri |
| Arial Black | Arial |
| Calibri | Calibri Light |
| Cambria | Calibri |
| Trebuchet MS | Calibri |
| Impact | Arial |
| Palatino | Garamond |

| Element | Size |
|---------|------|
| Slide title | 36–44pt bold |
| Section header | 20–24pt bold |
| Body | 14–16pt |
| Captions | 10–12pt muted |

## 间距

- 距边缘 ≥ 0.5"；块间距 0.3–0.5"；留白，不要填满。

## 避免（常见错误）

- 不要每页同一版式；不要正文居中（仅标题可居中）
- 不要字号对比不足；不要默认蓝；不要随机混用间距
- 不要只精修一页；不要纯文字页
- 与形状对齐时文本框设 `margin: 0`
- 不要低对比；**NEVER** 在标题下加装饰线（AI 幻灯片典型特征）
