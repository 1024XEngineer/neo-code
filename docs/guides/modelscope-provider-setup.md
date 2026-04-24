# ModelScope Provider 半引导配置

`modelscope` 已作为内置 provider 提供，并支持在 TUI 中走半引导流程获取 token。

## 触发方式

1. 在 TUI 输入 `/provider`
2. 选择 `modelscope`
3. 若未检测到 `MODELSCOPE_API_KEY`，会自动进入引导面板

## 引导流程

1. 优先打开本地指导页：`~/.neocode/modelscope-guide.html`
2. 打开登录页：<https://www.modelscope.cn/>
3. 打开 Token 页：<https://www.modelscope.cn/my/access/token>
4. 在 TUI 引导面板粘贴 token 并提交校验

如果返回认证或权限类错误，会自动回退并打开阿里云认证页：
<https://www.modelscope.cn/my/settings/account>

## 安全说明

- 不自动填充账号密码
- 不绕过验证码或实名认证
- token 由用户手工粘贴
- 配置不写入明文 token，仅保存环境变量名
