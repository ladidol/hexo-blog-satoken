package org.cuit.epoch.dto;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * @author: ladidol
 * @date: 2022/11/18 20:09
 * @description:
 */
@Data
@Builder
@AllArgsConstructor
@NoArgsConstructor
public class EmailDTO {

    /**
     * 邮箱号
     */
    private String email;

    /**
     * 主题
     */
    private String subject;

    /**
     * 内容
     */
    private String content;

}
