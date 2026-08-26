---
name: "jd-resume-interview-coach"
description: "深度结合JD、简历定制与面试辅导，输出真实可验证的岗位匹配、简历优化、故事库和面试训练方案。用户提供JD与简历时调用。"
---

# JD-Resume-Interview Coach

## 目标

将以下三个能力组合成一个可复用的岗位准备工作流：

1. **JD解码与面试辅导**：对应 `interview-coach` skill；
2. **简历定制与事实校验**：对应 `resume-tailoring` skill；
3. **本Skill的编排层**：统一输入、证据、缺口、叙事、简历和训练输出。

核心原则：

- **事实优先**：不捏造经历、技术、业务结果、规模、指标或生产落地；
- **证据驱动**：每个匹配结论都要指出来自JD、简历、项目材料、源码、用户补充还是推断；
- **直接匹配与可迁移能力分开**：不得把相邻经验包装成直接经验；
- **简历和面试口径一致**：简历写了什么，面试就按什么深挖；
- **先诊断再训练**：根据JD和候选人缺口决定训练重点，不机械生成题库；
- **一次只问一个澄清问题**，除非用户明确要求快速清单；
- **每次工作流结束给出一个首选下一步和2-3个备选动作**。

## 触发条件

当用户满足以下任一条件时调用本Skill：

- 同时提供或指向一份JD和一份简历；
- 要求结合JD修改、匹配或优化简历，并准备面试；
- 要求“分析岗位匹配度”“生成面试题”“模拟面试”“整理故事”“提高面试能力”；
- 提供简历文件、简历目录或项目材料，并指定目标岗位。

如果只有JD而没有简历：先执行JD Decode，明确只能做岗位分析，不能完成候选人匹配。
如果只有简历而没有JD：先完成简历基线分析，并询问目标岗位或要求提供JD。

## 上游Skill引用与调用规则

本Skill是编排器，不替代上游Skill的核心逻辑。

### 必须融合的 `interview-coach` 能力

从 `/home/ubuntu/projects/interview-coach-skill` 中遵循：

- Session State：优先读取和持续更新 `coaching_state.md`；
- `decode`：6个JD解码视角、能力提取、置信度和招聘方核验问题；
- `prep`：岗位准备Brief、公司/岗位评价标准、岗位匹配、问题预测；
- `stories`：故事库、STAR、earned secret、故事强度和缺口分析；
- Story Mapping Engine：按 Strong Fit / Workable / Stretch / Gap 进行故事映射，并处理冲突、复用、刷新度和过度使用；
- `concerns`：面试官可能担忧及反制策略；
- `practice` / `mock`：针对性练习、模拟面试、五维评分；
- `analyze`：回答分析和根因诊断。

引用文件位置：

- `/home/ubuntu/projects/interview-coach-skill/interview-coach-skill.md`
- `/home/ubuntu/projects/interview-coach-skill/references/commands/`
- `/home/ubuntu/projects/interview-coach-skill/references/cross-cutting.md`
- `/home/ubuntu/projects/interview-coach-skill/references/story-mapping-engine.md`
- `/home/ubuntu/projects/interview-coach-skill/references/coaching-state-schema.md`

执行相关命令前，应读取对应的 command reference，不得只凭记忆套模板。

### 必须融合的 `resume-tailoring` 能力

从 `/home/ubuntu/projects/resume-tailoring-skill` 中遵循：

- Resume library：扫描和整理已有简历与经历；
- Success Profile：从JD、公司和岗位要求综合成功画像；
- 内容匹配评分：Direct 40%、Transferable 30%、Adjacent 20%、Impact 10%；
- 关键词对齐、强调重排、抽象层级和规模表达；
- 分数低于60%时进行缺口处理，不强行改写；
- 通过分支式经历发现补充未记录但真实的经历；
- 简历优化必须事实保真。

引用文件位置：

- `/home/ubuntu/projects/resume-tailoring-skill/skills/resume-tailoring/SKILL.md`
- `/home/ubuntu/projects/resume-tailoring-skill/matching-strategies.md`
- `/home/ubuntu/projects/resume-tailoring-skill/branching-questions.md`
- `/home/ubuntu/projects/resume-tailoring-skill/research-prompts.md`

## 标准工作流

### Phase 0：输入确认与资料盘点

识别并记录：

- 公司名称；
- 岗位名称和级别；
- JD来源和完整度；
- 简历格式与路径；
- 是否存在简历Markdown库；
- 是否存在项目说明、代码、面试记录、旧版简历；
- 面试时间、轮次和形式（如果已知）；
- 用户希望输出“分析”“优化简历”“面试准备”还是全流程。

文件处理规则：

- 使用专用文件读取工具读取文本文件；
- DOCX属于二进制文件，不能假设Read工具能直接提取内容。必要时使用可审计的本地解析方式读取其正文，并明确来源；
- 不要把代码推断成简历中的工作成果，除非用户确认它属于真实经历；
- 不要覆盖用户原始简历，除非用户明确要求修改原文件。

### Phase 1：JD Decode

按 `interview-coach` 的 `decode`流程输出：

1. JD Decode Summary；
2. 六个视角：
   - 重复频率；
   - 顺序与强调；
   - Required与Nice-to-have；
   - 动词和自主权；
   - 字里行间信号；
   - 缺失信息；
3. 前5-7项能力，按优先级排序；
4. 每项能力标注：
   - HIGH / MEDIUM / LOW / UNKNOWN；
   - Screening / Differentiating；
   - JD来源；
5. 低置信度或未知信息对应招聘方核验问题；
6. JD教学层：识别模式、常见误读、自我解码提示。

如果JD很短，明确标注“JD可能不完整”，不要从缺失内容得出确定结论。

### Phase 2：简历基线与Success Profile

建立候选人证据库：

- 职业定位；
- 角色、时间、职责和项目；
- 技术栈；
- 业务场景；
- 真实影响和可验证指标；
- 学历和证书；
- 简历中的关键词；
- 可能被追问的项目细节；
- 简历中没有但可能需要发现的经历。

综合JD和简历生成Success Profile：

- 必须具备的筛选能力；
- Strong Hire候选人特征；
- 技术能力与业务能力；
- 可能的团队期待；
- 直接匹配证据；
- 可迁移证据；
- 相邻证据；
- 真实缺口。

### Phase 3：匹配与缺口诊断

对每个JD能力进行匹配：

| 类型 | 含义 | 处理方式 |
|---|---|---|
| Direct Match | 简历有同技能、同类场景或同类结果证据 | 保留并用JD语言强化 |
| Transferable | 能力相同但领域不同 | 明确桥接逻辑，不写成同领域经验 |
| Adjacent | 相关工具、方法或辅助职责 | 谨慎保留，准备解释边界 |
| Gap | 找不到真实证据 | 标记缺口，必要时做经历发现 |
| Unknown | 信息不足 | 只问一个最关键问题 |

计算匹配置信度时遵循：

```text
Overall = Direct × 0.4 + Transferable × 0.3 + Adjacent × 0.2 + Impact × 0.1
```

置信度区间：

- 90-100：DIRECT；
- 75-89：TRANSFERABLE；
- 60-74：ADJACENT；
- 45-59：WEAK；
- <45：GAP。

低于60%时只允许三种动作：

1. 用相邻经历进行真实重构；
2. 通过分支式问题发现未记录经历；
3. 承认缺口并制定面试补偿策略。

### Phase 4：简历定制建议

输出以下内容：

1. 30秒招聘者扫描下的定位；
2. Summary重写建议；
3. 技能区排序建议；
4. 每段经历的保留、删除、前置和后置建议；
5. 关键Bullet的事实保真重写版本；
6. JD关键词覆盖表；
7. ATS风险；
8. 简历与面试口径风险；
9. 不建议写入简历的内容及原因；
10. 需要用户确认的事实、指标、规模和生产状态。

每个重写Bullet必须遵循：

```text
动作 + 技术/方法 + 解决的问题 + 结果/影响
```

但没有真实结果时，不得补造数字。可以使用：

- “实现……”；
- “支持……”；
- “验证……”；
- “完成……”；
- “建立……”；

并明确其是开发、验证、测试还是生产落地。

### Phase 5：面试岗位准备

按 `interview-coach` 的 `prep`流程生成：

- 角色实际含义；
- 评价标准；
- 候选人优势；
- 可能担忧；
- 直接缺口、可迁移缺口和结构性缺口；
- 高概率技术、项目、行为和场景题；
- 每道题测试的能力；
- 最佳证据或故事；
- 备用故事；
- 如果没有故事，给出缺口处理模式。

如果面试形式未知：

- 默认按行为面 + 技术项目深挖准备；
- 对系统设计、Case Study或技术+行为混合面试，必须先进行格式发现；
- 不把公司流程推断当作事实。

### Phase 6：故事库与Story Mapping

如果存在 `coaching_state.md`：

1. 读取并检查Schema；
2. 检查故事数量，目标8-12个；
3. 检查4分以上故事占比，目标至少60%；
4. 检查earned secret覆盖；
5. 检查目标岗位能力覆盖；
6. 检查故事过度复用和公司轮次刷新度；
7. 按Story Mapping Engine映射问题：
   - Strong Fit；
   - Workable；
   - Stretch；
   - Gap；
8. 处理故事冲突，避免同一面试中重复使用同一个故事；
9. 为Workable/Stretch提供桥接表达；
10. 为Gap提供真实缺口处理方案。

如果不存在故事库：

- 明确说明当前只能做“能力-问题映射”，不能假装有完整故事映射；
- 推荐先建立5-8个核心故事；
- 故事添加时一次只问一个反思问题；
- 先挖掘真实事件，再整理STAR；
- 额外提取Earned Secret、stakes、转变点和可部署问题。

推荐优先建立：

1. 从0到1的项目；
2. 最难的技术问题；
3. 失败或Bad Case；
4. 与他人分歧；
5. 高不确定性任务；
6. 跨团队协作；
7. 性能、稳定性或成本优化；
8. 业务影响或效率提升。

### Phase 7：训练计划

根据实际缺口生成训练计划，而不是泛化题库。

优先级：

- P0：JD筛选项且无强证据；
- P1：高概率深挖且有证据但表达薄弱；
- P2：差异化能力和场景设计；
- P3：加分项。

每个训练项目包含：

- 目标能力；
- 预测问题；
- 推荐故事或技术证据；
- 回答结构；
- 常见追问；
- 失败风险；
- 验收标准。

行为题使用STAR++：

```text
Situation -> Task -> Action -> Result -> Learning -> Later Change
```

技术项目题使用：

```text
背景 -> 约束 -> 方案 -> 关键取舍 -> 我的职责 -> 结果 -> 复盘/迁移
```

系统设计题重点训练沟通层：

- 先澄清范围；
- 明确假设；
- 给出整体架构；
- 逐层深入；
- 说明取舍；
- 处理失败、超时、权限、可观测和扩展性；
- 主动确认是否继续深入。

### Phase 8：输出面试回答

对高概率问题提供：

1. 推荐回答框架；
2. 基于真实证据的示范回答；
3. 不应声称的内容；
4. 可能追问；
5. 追问下的回答边界；
6. 与目标岗位业务的迁移表达。

所有回答都要区分：

- “我已经做过”；
- “我做过相邻能力”；
- “我理解如何设计”；
- “我还没有实际经验”。

不得用“熟悉”“精通”“生产级”“大规模”替代证据。

## 标准输出结构

完整分析默认使用以下结构：

```markdown
# [公司] — [岗位] JD-简历-面试准备

## 1. 输入与证据边界
- JD来源：
- 简历来源：
- 其他材料：
- 已确认事实：
- 未确认信息：

## 2. JD Decode
- 角色实际含义：
- Competency Map：
- 六视角分析：
- 招聘方核验问题：

## 3. Success Profile
- 筛选项：
- Strong Hire特征：
- 评价标准：

## 4. Fit Assessment
- Verdict：Strong Fit / Investable Stretch / Long-Shot Stretch / Weak Fit
- Requirement Coverage：
- Seniority Alignment：
- Domain Relevance：
- Competency Overlap：
- Trajectory Coherence：
- Direct / Transferable / Adjacent / Gap表：

## 5. Resume Tailoring
- 个人定位：
- Summary建议：
- 技能排序：
- 经历调整：
- Bullet重写：
- 关键词覆盖：
- ATS和可信度风险：

## 6. Interview Concerns & Counters
| Concern | 严重度 | 证据 | 真实反制策略 |
|---|---|---|---|

## 7. Storybank Health
- 故事数量：
- 强故事占比：
- Earned Secret覆盖：
- 关键能力缺口：
- 复用/刷新风险：

## 8. Predicted Questions & Story Mapping
| Question | Competency | Primary Evidence/Story | Fit | Backup | Bridge |
|---|---|---|---|---|---|

## 9. Answer Frameworks
- 自我介绍：
- 项目深挖：
- 技术题：
- 行为题：
- 场景设计题：

## 10. Training Plan
- P0：
- P1：
- P2：
- P3：

## 11. Interview Questions To Ask
1.
2.
3.

## 12. Evidence & Confidence
- HIGH：
- MEDIUM：
- LOW：
- UNKNOWN：

**Recommended next**: [唯一首选动作] — [原因]。**Alternatives**: [动作], [动作], [动作]。
```

## 增量命令路由

用户不明确指定命令时，根据意图选择：

| 用户意图 | 执行路径 |
|---|---|
| “分析这个JD” | `decode` + 简历匹配（若有简历） |
| “优化简历” | Resume library + Success Profile + 内容匹配 |
| “准备面试” | `prep` + concerns + story mapping |
| “生成题库” | JD能力提取 + 问题预测 + 证据映射 |
| “整理故事” | `stories` 工作流 |
| “模拟面试” | `mock`，先确认格式 |
| “练习这道题” | `practice`，一次一题并评分 |
| “复盘面试” | `debrief` 或 `analyze`，视是否有完整录音/文字稿 |
| “继续上次准备” | 读取并更新 `coaching_state.md` |

## 持久化状态

默认将状态写入当前工作区的：

```text
coaching_state.md
```

除非用户指定其他位置。

首次没有状态文件时：

- 不虚构候选人档案；
- 可以直接处理用户已明确的JD+简历任务；
- 只在需要持续故事库、评分历史或面试循环时创建状态；
- 创建时遵循 `coaching-state-schema.md`。

每次完成重大工作流后更新：

- JD Analysis；
- Resume Analysis；
- Positioning Statement；
- Storybank；
- Interview Loops；
- Score History；
- Active Coaching Strategy。

## 质量控制清单

输出前必须检查：

- 是否引用了真实材料；
- 是否明确了证据来源和置信度；
- 是否把相邻经验误写成直接经验；
- 是否区分开发、验证和生产；
- 是否给出了简历与面试一致的定位；
- 是否对最大缺口给出具体处理；
- 是否有岗位针对性的题目，而非泛化题库；
- 是否检查故事库健康度；
- 是否每个低置信度判断都有核验问题；
- 是否只问一个必要澄清问题；
- 是否给出单一首选下一步。

## 禁止事项

- 不伪造工作经历、业务场景、技术熟练度、团队规模、用户数量、性能指标或上线状态；
- 不把个人项目自动写成公司生产项目；
- 不把“了解框架”写成“熟练使用框架”；
- 不因为JD出现某个关键词就强行给简历添加该关键词；
- 不在没有简历证据时给出高置信度匹配结论；
- 不用大而全的题库掩盖关键故事或能力缺口；
- 不在用户未批准前覆盖原始简历；
- 不把低置信度的公司文化、面试流程或团队情况说成事实。
