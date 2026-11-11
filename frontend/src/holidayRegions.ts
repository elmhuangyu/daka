// Holiday regions: "CN", or a Nager.Date country ("US", "CA") optionally with a
// subdivision ("CA-ON"). Must match holiday.ValidRegion on the backend.
export type Sub = { code: string; en: string; zh: string }

const sub = (code: string, en: string, zh: string): Sub => ({ code, en, zh })

export const SUBDIVISIONS: Record<string, Sub[]> = {
  CA: [
    sub('AB', 'Alberta', '阿尔伯塔'),
    sub('BC', 'British Columbia', '不列颠哥伦比亚'),
    sub('MB', 'Manitoba', '马尼托巴'),
    sub('NB', 'New Brunswick', '新不伦瑞克'),
    sub('NL', 'Newfoundland and Labrador', '纽芬兰与拉布拉多'),
    sub('NS', 'Nova Scotia', '新斯科舍'),
    sub('NT', 'Northwest Territories', '西北地区'),
    sub('NU', 'Nunavut', '努纳武特'),
    sub('ON', 'Ontario', '安大略'),
    sub('PE', 'Prince Edward Island', '爱德华王子岛'),
    sub('QC', 'Quebec', '魁北克'),
    sub('SK', 'Saskatchewan', '萨斯喀彻温'),
    sub('YT', 'Yukon', '育空'),
  ],
  US: [
    sub('AL', 'Alabama', '阿拉巴马'),
    sub('AK', 'Alaska', '阿拉斯加'),
    sub('AZ', 'Arizona', '亚利桑那'),
    sub('AR', 'Arkansas', '阿肯色'),
    sub('CA', 'California', '加利福尼亚'),
    sub('CO', 'Colorado', '科罗拉多'),
    sub('CT', 'Connecticut', '康涅狄格'),
    sub('DE', 'Delaware', '特拉华'),
    sub('DC', 'District of Columbia', '哥伦比亚特区'),
    sub('FL', 'Florida', '佛罗里达'),
    sub('GA', 'Georgia', '佐治亚'),
    sub('HI', 'Hawaii', '夏威夷'),
    sub('ID', 'Idaho', '爱达荷'),
    sub('IL', 'Illinois', '伊利诺伊'),
    sub('IN', 'Indiana', '印第安纳'),
    sub('IA', 'Iowa', '艾奥瓦'),
    sub('KS', 'Kansas', '堪萨斯'),
    sub('KY', 'Kentucky', '肯塔基'),
    sub('LA', 'Louisiana', '路易斯安那'),
    sub('ME', 'Maine', '缅因'),
    sub('MD', 'Maryland', '马里兰'),
    sub('MA', 'Massachusetts', '马萨诸塞'),
    sub('MI', 'Michigan', '密歇根'),
    sub('MN', 'Minnesota', '明尼苏达'),
    sub('MS', 'Mississippi', '密西西比'),
    sub('MO', 'Missouri', '密苏里'),
    sub('MT', 'Montana', '蒙大拿'),
    sub('NE', 'Nebraska', '内布拉斯加'),
    sub('NV', 'Nevada', '内华达'),
    sub('NH', 'New Hampshire', '新罕布什尔'),
    sub('NJ', 'New Jersey', '新泽西'),
    sub('NM', 'New Mexico', '新墨西哥'),
    sub('NY', 'New York', '纽约'),
    sub('NC', 'North Carolina', '北卡罗来纳'),
    sub('ND', 'North Dakota', '北达科他'),
    sub('OH', 'Ohio', '俄亥俄'),
    sub('OK', 'Oklahoma', '俄克拉荷马'),
    sub('OR', 'Oregon', '俄勒冈'),
    sub('PA', 'Pennsylvania', '宾夕法尼亚'),
    sub('RI', 'Rhode Island', '罗得岛'),
    sub('SC', 'South Carolina', '南卡罗来纳'),
    sub('SD', 'South Dakota', '南达科他'),
    sub('TN', 'Tennessee', '田纳西'),
    sub('TX', 'Texas', '得克萨斯'),
    sub('UT', 'Utah', '犹他'),
    sub('VT', 'Vermont', '佛蒙特'),
    sub('VA', 'Virginia', '弗吉尼亚'),
    sub('WA', 'Washington', '华盛顿'),
    sub('WV', 'West Virginia', '西弗吉尼亚'),
    sub('WI', 'Wisconsin', '威斯康星'),
    sub('WY', 'Wyoming', '怀俄明'),
  ],
}

/** "CA-ON" -> ["CA", "CA-ON"]; "CN" -> ["CN", ""]; "" -> ["", ""]. */
export function splitRegion(region: string | undefined): [string, string] {
  if (!region) return ['', '']
  const i = region.indexOf('-')
  return i < 0 ? [region, ''] : [region.slice(0, i), region]
}
