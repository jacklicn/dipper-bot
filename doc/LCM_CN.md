# LCM 压缩对话历史并组装上下文

**语言**：本文为中文版；英文版见 [LCM.md](./LCM.md)。

LCM（Lossless Context Management，无损上下文管理）通过 **LLM 摘要** + **DAG 结构**，在 token 预算内尽可能保留长对话的关键信息。

- **LLM 摘要**：将长段对话交给大模型（LLM）生成简短摘要，保留关键事实、决策和上下文，用摘要替代原文以节省 token。
- **DAG 结构**：有向无环图（Directed Acyclic Graph），用于组织摘要层级，支持追溯与检索。详见下文。

#### DAG 结构原理

**图的构成**

- **节点**：原始消息（message）、叶子摘要（leaf summary）、浓缩摘要（condensed summary）。
- **边（有向）**：从**摘要指向其来源**——leaf 指向它覆盖的 messages；condensed 指向它合并的 leaf/condensed。存储时用 `summary_messages`、`summary_parents` 记录。

**方向与层级**

- **自底向上**：message → leaf → condensed → 更高层 condensed。时间上越早的内容越靠近底层。
- **无环**：摘要只基于“更早”的内容生成，不会出现 A 摘要 B、B 又摘要 A 的情况，因此图无环。

**作用**

1. **可追溯**：从 condensed 可沿边追溯到 leaf，再追溯到原始 message。
2. **可检索**：`lcm_grep` 在 DAG 上做正则搜索；`lcm_describe` 利用 summary 的 `earliest_at`、`latest_at` 等元数据快速定位时间范围。
3. **有序性**：`context_items` 按 `ordinal` 保持时间顺序，Assemble 时从新到旧选取，与 DAG 结构配合使用。

**示意**

```
                    [condensed]  ← 高层摘要，覆盖多个 leaf
                       ↑ ↑ ↑
         [leaf A] [leaf B] [leaf C]  ← 叶子摘要，各覆盖一段 message
            ↑         ↑         ↑
    [msg1..msg8] [msg9..msg16] [msg17..msg24]  ← 原始消息
```

---

## LCM 原理介绍

### 问题背景

大模型的上下文窗口有固定上限（如 128k token）。长对话会产生大量历史消息，超出窗口时需做截断：

- **滑动窗口截断**：只保留最近 N 条。旧消息完全丢弃，模型无法回溯早期关键事实、决策、约定。
- **固定 token 截断**：按 token 数从后往前保留。同样会丢失前半段对话。

### 核心思想

LCM 的核心理念是**「压缩而非丢弃」**：

1. **持久化全部消息**：所有对话先写入 SQLite，不因窗口限制而删除。
2. **分块摘要**：将较老的、连续的消息块交给 LLM 做摘要，生成 **leaf summary**（叶子摘要），用摘要替换原始消息。
3. **多级压缩**：多个 leaf summary 可进一步交给 LLM 做「二次摘要」，得到 **condensed summary**（浓缩摘要），形成层级结构。
4. **DAG 结构**：leaf 指向原始消息，condensed 指向多个 leaf，形成有向无环图。便于追溯摘要来源，也方便 `lcm_grep`、`lcm_describe` 等检索。
5. **按需组装**：在 token 预算内，从新到旧选取 summary 和 message，优先保留最近内容，用 summary 覆盖更早部分，从而在有限 token 内尽量保留整体脉络。

### 与滑动窗口的对比

| 维度 | 滑动窗口 | LCM |
|------|----------|-----|
| 旧消息 | 直接丢弃 | 摘要后保留，可检索 |
| 关键信息 | 易丢失（若在窗口外） | 通过摘要保留事实、决策、上下文 |
| token 占用 | 固定（最近 N 条） | 可调（summary 更省 token） |
| 长对话表现 | 遗忘早期内容 | 有层次的历史表示 |

### 设计参考

LCM 设计参考了 [lossless-claw](https://github.com/Martian-Engineering/lossless-claw)：通过 DAG 摘要替代简单截断，实现**无损**的上下文管理——不丢原始数据（存于 SQLite），只在外送 LLM 的 prompt 中用摘要替代长文本。

### 术语：叶子摘要与浓缩摘要

**叶子摘要（Leaf Summary）**

叶子摘要是 LCM 中的**第一层摘要**，对应 DAG 中最底层的节点，直接“挂在”原始消息之上。

- **Leaf**：指该摘要只基于**原始对话**（user/assistant 消息），不再基于其他摘要。
- **形成过程**：取一段连续的原始消息（≥ `LeafMinFanout` 条，默认 8），交给 LLM 做一次摘要，得到 leaf summary。
- **存储**：`summary_messages` 表记录该 leaf 对应哪些 `message_id`，形成 DAG 的叶子层。

```
原始消息 (message) ──► LLM 摘要 ──► leaf summary（叶子摘要）
     ↑                                      ↑
  sqlite messages                    sqlite summaries (kind='leaf')
```

**浓缩摘要（Condensed Summary）**

浓缩摘要是**第二层及以上的摘要**，其输入不是原始消息，而是多个 leaf summary（或已有的 condensed）。

- **形成过程**：当出现 ≥ `CondensedMinFanout` 个连续的 leaf 时，将这些 leaf 交给 LLM 做“二次摘要”。
- **层级**：leaf 为 depth=0，condensed 为 depth≥1；多个 condensed 可继续合并，形成更高层节点。

| 类型 | 输入 | 层级 |
|------|------|------|
| Leaf summary | 原始对话消息 | 第 0 层，叶子节点 |
| Condensed summary | 多个 leaf summary | 第 1 层及以上，父节点 |

---

## 一、整体流程

```
对话消息 → SQLite 持久化 → 触发 Compaction（摘要） → Assemble（组装） → 给 LLM 的上下文
```

---

## 二、数据存储（SQLite）

LCM 使用 SQLite（`workspace/lcm.db`）持久化所有对话：

- **conversations**：会话元数据
- **messages**：原始消息（seq、role、content、token_count）
- **summaries**：摘要节点（leaf 或 condensed）
- **context_items**：有序的上下文条目列表，每项是 `message` 或 `summary`，按 `ordinal` 排序

每条新消息先写入 `messages`，并在 `context_items` 中追加一项。

---

## 三、压缩（Compaction）

### 3.1 触发时机

`engine.go` 第 135-174 行：每次 `IngestTurn` 后检查总 token，超过阈值则运行增量压缩：

```go
total, _ := e.store.GetTotalTokenCount(convID)
threshold := int(float64(128000) * e.cfg.ContextThreshold)  // 默认 0.75 ≈ 96k
if total > threshold {
    e.comp.RunIncremental(ctx, convID)
}
```

### 3.2 Leaf 压缩（compaction.go）

1. **选取可压缩范围**
   - 保留最近 `freshTailCount`（默认 32）条 context_items 不动（fresh tail）
   - 在 evictable 前缀中，找** oldest contiguous chunk of raw messages**（最老的、连续的原始消息块）

2. **最少消息数**
   - 若该块消息数 < `leafMinFanout`（默认 8），则本次不压缩

3. **Token 裁剪**
   - 若块的总 token > `leafChunkTokens`（默认 20k），从尾部裁剪至 ≤ 20k

4. **LLM 摘要**
   - 将消息块拼成文本，调用 `summarize`（即 LLM）生成摘要
   - Prompt: `Summarize the following conversation transcript. Preserve key facts, decisions, and context...`
   - 目标 token 数：`LeafTargetTokens`（默认 1200）

5. **替换**
   - 用一条 `summary` 替换 `context_items` 中该区间的多条 `message`
   - 记录 `summary_messages` 映射，形成 DAG 的叶子节点

6. **Condensation（多级摘要）**
   - `runCondensation`：在 evictable 前缀中查找**连续的 leaf summary 段**，当数量 ≥ `CondensedMinFanout`（默认 4）时，调用 LLM 用 `condensedPrompt` 生成更高层摘要，并用一条 `condensed` 节点替换该段
   - 输入 token 超过 8000 时从尾部裁剪，保留最老的内容
   - 创建的 condensed 节点记录 `ParentSummaryIDs`，形成 DAG 的父节点引用

---

## 四、上下文组装（Assemble）

`assembler.go` 的 `Assemble` 函数：

1. **分区**
   - **Protected tail**：最近 `freshTailCount` 条（默认 32），**始终全部保留**
   - **Evictable prefix**：前面所有 summary + message

2. **Token 预算**
   ```go
   threshold := int(float64(maxContextTokens) * cfg.ContextThreshold)
   budget := threshold - tailTokens  // 预算 = 阈值 - tail 已占用
   ```

3. **从新到旧填充 budget**
   - 从 fresh tail 的前一条开始，向前遍历 evictable 区
   - 按 token 占用顺序塞入，直到 budget 用尽或遍历完
   - 越新的内容越优先保留

4. **最终序列**
   - 输出：`[evictable 中被选中的部分] + [fresh tail]`

---

## 五、输出格式

- **原始消息**：直接作为 `role` + `content` 送入 LLM
- **Summary**：包在 XML 中，带元数据：

```xml
<summary id="sum_xxx" kind="leaf" depth="0" descendant_count="0" 
         earliest_at="..." latest_at="...">
<content>
摘要内容...
</content>
</summary>
```

`ItemsToRoleContent` 将 summary 转为 `user` 角色的消息供 LLM 消费。

---

## 六、在 Agent 中的使用流程

1. **Bootstrap**（loop.go 354-356）：  
   若 session 有 JSONL 历史、LCM 尚未导入，则调用 `lcm.Bootstrap` 将历史消息批量导入 SQLite

2. **BuildMessages**（context.go 141-145）：  
   若启用 LCM，用 `lcm.AssembleContext(ctx, convID, maxTokens)` 替代滑动窗口历史

3. **IngestTurn**（loop.go 385-387）：  
   每轮对话结束后，把本轮的 user/assistant 消息传给 `lcm.IngestTurn`，持久化并可能触发 compaction

---

## 七、关键配置（config.go）

| 参数 | 默认 | 含义 |
|------|------|------|
| `ContextThreshold` | 0.75 | 总 token 超过 128k×0.75 时触发 compaction |
| `FreshTailCount` | 32 | 最近 N 条不压缩、始终保留 |
| `LeafMinFanout` | 8 | 至少 8 条消息才做 leaf 压缩 |
| `LeafChunkTokens` | 20000 | 单次 leaf 压缩的最大源 token |
| `LeafTargetTokens` | 1200 | leaf 摘要的目标 token 数 |
| `CondensedMinFanout` | 4 | 至少 N 个 leaf summary 才做 condensed 压缩 |
| `CondensedTargetTokens` | 2000 | condensed 摘要的目标 token 数 |

---

## 八、总结

LCM 的核心思路：把**旧消息**用 LLM 摘要成 **leaf summary** 存进 SQLite，在组装上下文时**优先保留最近消息**，在 token 预算内从新到旧填充 summary/message，从而在不丢关键信息的前提下控制上下文长度。
