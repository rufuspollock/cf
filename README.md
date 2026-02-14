# `cf`: cloudflare command line

Register domain names through cloudflare on the command line like vercel does.

Goal is “Vercel-like DX”, the clean solution is:

Write a small Node/Go/Rust CLI

Add commands like:

```
cf domains search example.com
cf domains register example.com
cf domains dns add example.com A 1.2.3.4
```

This aligns well with our stack like e.g. FlowerShow, DataHub etc.
