# 智哨 Portal（React + Ant Design）

M5 前端脚手架，对接 `/api/v1` 后端。

## 开发

```bash
cd portal
npm install
npm run dev
```

浏览器访问 http://127.0.0.1:5173

- 默认账号：`sre@example.com` / `dev123`
- Vite 代理：`/api/v1` → `http://127.0.0.1:8090`（需先启动后端）

## 已实现

| 页面 | 状态 |
|------|------|
| 登录 | ✅ `POST /auth/token` |
| 知识库 | ✅ 上传 / 列表 / 删除 |
| 对话 | 🔶 UI 原型（M3 接入 SSE） |
| 告警分析 | 🔶 UI 原型（M4 接入） |
| 审批 / 管理 | 🔶 UI 占位 |

## 构建

```bash
npm run build
npm run preview
```

## 目录

```
src/
├── api/          # API 客户端
├── components/   # 通用组件
├── context/      # Auth
├── layouts/      # 侧栏布局
├── pages/        # 页面
└── theme/        # Design Tokens
```

原型参考：[`portal-prototype/`](../portal-prototype/) · Figma 规范：[`docs/WiseSentinel-Portal-Figma原型设计规范.md`](../docs/WiseSentinel-Portal-Figma原型设计规范.md)
