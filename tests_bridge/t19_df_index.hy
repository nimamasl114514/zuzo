// t19 py取值(df, "records+index")：DataFrame groupby 的城市索引不再丢（第33轮 · 问题2）
// 期望（python -c 现算）：
//   python -c "import pandas as pd; s=pd.DataFrame({'城市':['北京','上海','上海'],'销量':[3,5,2]}).groupby('城市').sum(); print(list(s.index), s['销量'].to_dict())"
//   → ['上海', '北京'] {'上海': 7, '北京': 3}   （groupby 默认 sort=True，按城市名字典序）
// 对照旧行为：py取值(汇总) 只有记录、索引丢弃。
变量 pd = py导入("pandas")
变量 df = pd.DataFrame({"城市": ["北京", "上海", "上海"], "销量": [3, 5, 2]})
变量 汇总 = df.groupby("城市").sum()
变量 结果 = py取值(汇总, "records+index")
打印("[t19] 索引=", 结果["索引"], " 记录=", 结果["记录"])
打印("[t19] 旧行为=", py取值(汇总), " 记录数=", 长度(结果["记录"]))
