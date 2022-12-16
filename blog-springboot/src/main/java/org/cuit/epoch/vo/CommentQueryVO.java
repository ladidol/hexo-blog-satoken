package org.cuit.epoch.vo;

import io.swagger.annotations.ApiModel;
import io.swagger.annotations.ApiModelProperty;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import javax.validation.constraints.NotNull;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/14 21:39
 * @description: {}
 */
@Data
@Builder
@AllArgsConstructor
@NoArgsConstructor
@ApiModel(description = "评论")
public class CommentQueryVO {


    /**
     * 评论主题id
     */
    @ApiModelProperty(name = "topicId", value = "主题id", dataType = "Integer")
    private Integer topicId;


    /**
     * 父评论id
     */
    @ApiModelProperty(name = "parentId", value = "评论父id", dataType = "Integer")
    private Integer parentId;

    /**
     * 类型
     */
    @NotNull(message = "评论类型不能为空")
    @ApiModelProperty(name = "type", value = "评论类型", dataType = "Integer")
    private Integer type;

}