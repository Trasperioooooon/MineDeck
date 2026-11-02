# MineDeck Companion：许可说明

- 本模组的代码按 MineDeck 仓库的 MIT 许可发布。
- 本模组运行时依赖下面两个模组，但 **jar 里不包含它们的任何文件**，也没有修改它们：
  - PlayerEngine（https://github.com/shakey2/PlayerEngine 等），GNU LGPL-3.0（仓库的 LICENSE 文件和模组元数据都写 LGPL-3.0）。
    PlayerEngine 内嵌了 Automatone / Baritone 的代码（LGPL-3.0）。
  - Architectury API（https://github.com/architectury/architectury-api），GNU LGPL-3.0。
- 本模组只通过公开的类和方法调用 PlayerEngine（按 LGPL-3.0 第 4 条"组合作品"的方式使用），
  用户可以把 PlayerEngine 换成任何兼容的版本（包括自己改过的版本）。
