package org.cuit.epoch.enums;

import lombok.AllArgsConstructor;
import lombok.Getter;

/**
 * @author: ladidol
 * @date: 2022/11/16 17:20
 * @description:
 */
@Getter
@AllArgsConstructor
public enum ZoneEnum {

    /**
     * 上海
     */
    SHANGHAI("Asia/Shanghai", "中国上海");

    /**
     * 时区
     */
    private final String zone;

    /**
     * 描述
     */
    private final String desc;

}