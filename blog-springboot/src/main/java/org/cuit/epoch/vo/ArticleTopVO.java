package org.cuit.epoch.vo;

import io.swagger.annotations.ApiModel;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import javax.validation.constraints.NotNull;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/17 22:16
 * @description: {}
 */
@Data
@AllArgsConstructor
@NoArgsConstructor
@Builder
@ApiModel(description = "文章置顶")
public class ArticleTopVO {

    /**
     * id
     */
    @NotNull(message = "id不能为空")
    private Integer id;

    /**
     * 置顶状态
     */
    @NotNull(message = "置顶状态不能为空")
    private Integer isTop;

}