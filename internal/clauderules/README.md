# Community Claude rules

This package downloads [v2fly/domain-list-community's Anthropic list](https://github.com/v2fly/domain-list-community/blob/master/data/anthropic) at a fixed Git commit. It does not embed the upstream data or execute upstream code. The project classifies domains; it does not endorse any routing decision, and the list is not published by Anthropic.

Plain domains and `domain:` retain suffix matching; `full:` retains exact matching. Includes, attributes, affiliations, keywords, regular expressions and invalid domains reject the entire update. Failed downloads or parsing must leave the caller's previous snapshot intact.

The fetched community data is distributed under the following [upstream MIT license](https://github.com/v2fly/domain-list-community/blob/master/LICENSE); retain this notice when redistributing copies or substantial portions of that data:

MIT License

Copyright (c) 2018-2019 V2Ray

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
