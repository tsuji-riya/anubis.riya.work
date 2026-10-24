# anubis.riya.work

[anubis.riya.work](https://anubis.riya.work) の静的サイトです。[Anubis](https://github.com/TecharoHQ/anubis) のチェッカーを通ると、Anubis ちゃんを無限に出し続けます。

このリポジトリにあるのはサイト本体 (`site/`) と、それを配る nginx のイメージだけです。前段の Anubis とそのポリシー (`botPolicy.yaml`) は [tsuji-riya/lab](https://github.com/tsuji-riya/lab) の `apps/anubis/` にあります。

## 手元で動かす

```sh
docker build -t anubis.riya.work . && docker run --rm -p 8080:8080 anubis.riya.work
```

http://localhost:8080 を開きます。手元では Anubis を通らないので、チェッカーは出ません。

## リリース

main に push するたびに GitHub Actions が `ghcr.io/tsuji-riya/anubis.riya.work:1.0.<実行番号>` (amd64 / arm64) を作ります。
デプロイは lab の `apps/anubis/` で行い、新しいタグは lab の Dependabot が更新 PR にします。

## ライセンス

このリポジトリのコードは GPL-3.0 です。`site/` の画像は CELPHASE 作で、MIT ライセンスです (`site/LICENSE.txt`)。
