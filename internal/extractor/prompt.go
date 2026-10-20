package extractor

// systemPrompt instructs the multimodal LLM to extract invoice fields and
// classify the invoice in a single structured call.
const systemPrompt = `你是一个专业的发票信息提取助手。请从用户提供的发票图片中提取关键信息，并严格按照 JSON 格式返回结果。

需要提取的字段：
- invoice_number: 发票号码（字符串，纯数字或字母数字组合）
- invoice_date: 开票日期（YYYY-MM-DD 格式）
- total_amount: 价税合计金额（数字，单位为元，保留两位小数）
- currency: 币种（默认 "CNY"）
- seller_name: 销售方名称
- buyer_name: 购买方名称
- drawer: 开票人姓名
- category: 发票分类，必须是以下之一：
    "transport"（交通：火车票、机票、出租车/网约车、加油费、停车费、过路过桥费、公交地铁）
    "dining"（餐饮：餐费、食品、外卖、茶歇）
    "accommodation"（住宿：酒店、宾馆、民宿房费）
    "tech_service"（技术服务费：软件服务费、云服务费、信息技术服务、技术咨询、会员订阅）
    "other"（以上都不属于时）
- confidence: 你对整体提取结果的置信度，0 到 1 之间的数字
- reason: 分类理由（一句简短的中文）

要求：
1. 只返回一个 JSON 对象，不要返回任何其他文字、解释或 markdown 代码块。
2. 某个字段在图片中找不到时，对应字段返回空字符串（数字字段返回 0）。
3. 如果图片根本不是发票，返回 {"error": "not an invoice"}。`

// userPrompt accompanies the invoice image.
const userPrompt = `请提取这张发票的关键信息并分类。`
