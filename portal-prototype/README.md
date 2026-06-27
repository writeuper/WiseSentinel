# 智哨 Portal 交互原型

高保真 HTML 原型，供 Figma 搭建与 M5 前端开发参考。

## 预览

```bash
# 在项目根目录
xdg-open portal-prototype/index.html
# 或
python3 -m http.server 8080 --directory portal-prototype
# 浏览器访问 http://127.0.0.1:8080
```

## 页面

| Tab | 说明 |
|-----|------|
| 登录 | 开发登录（对应 `/api/v1/auth/token`） |
| 对话 | ChatGPT 式布局 + RAG 引用 + 工具调用 |
| 告警分析 | Ops Plan-Execute 报告与时间线 |
| 知识库 | 上传区 + 文档列表（M2 API 已就绪） |
| 审批中心 | 待审批队列 |
| 管理 | Agent 配置版本 |

## 导入 Figma

1. 浏览器打开本原型，逐页截图或使用 [html.to.design](https://html.to.design/) 插件
2. 对照 [`docs/WiseSentinel-Portal-Figma原型设计规范.md`](../docs/WiseSentinel-Portal-Figma原型设计规范.md) 建立 Design Tokens 与组件库
