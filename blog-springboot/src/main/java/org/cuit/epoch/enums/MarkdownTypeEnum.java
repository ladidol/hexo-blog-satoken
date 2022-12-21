package org.cuit.epoch.enums;

import lombok.AllArgsConstructor;
import lombok.Getter;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/21 18:44
 * @description: {}
 */
@Getter
@AllArgsConstructor
public enum MarkdownTypeEnum {
    /**
     * 普通文章
     */
    NORMAL("", "normalArticleImportStrategyImpl"),
    /**
     * Hexo文章
     */
    HEXO("hexo", "hexoArticleImportStrategyImpl");

    /**
     * 类型
     */
    private final String type;

    /**
     * 策略
     */
    private final String strategy;

    public static String getMarkdownType(String name) {
        if (name == null) {
            return NORMAL.getStrategy();
        }
        for (MarkdownTypeEnum value : MarkdownTypeEnum.values()) {
            if (value.getType().equalsIgnoreCase(name)) {
                return value.getStrategy();
            }
        }
        return null;
    }
}
