package org.cuit.epoch.vo;

import io.swagger.annotations.ApiModel;
import io.swagger.annotations.ApiModelProperty;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import javax.validation.constraints.NotBlank;
import javax.validation.constraints.NotNull;

/**
 * @author: ladidol
 * @date: 2022/11/25 14:26
 * @description:
 */
@Data
@AllArgsConstructor
@NoArgsConstructor
@Builder
@ApiModel(description = "菜单")
public class MenuVO {

    /**
     * id
     */
    @ApiModelProperty(name = "id", value = "菜单id", dataType = "Integer")
    private Integer id;

    /**
     * 菜单名
     */
    @NotBlank(message = "菜单名不能为空")
    @ApiModelProperty(name = "name", value = "菜单名", dataType = "String",required = true)
    private String name;

    /**
     * icon
     */
    @NotBlank(message = "菜单icon不能为空")
    @ApiModelProperty(name = "icon", value = "菜单icon", dataType = "String",required = true)
    private String icon;

    /**
     * 路径
     */
    @NotBlank(message = "路径不能为空")
    @ApiModelProperty(name = "path", value = "路径", dataType = "String",required = true)
    private String path;

    /**
     * 组件
     */
    @NotBlank(message = "组件不能为空")
    @ApiModelProperty(name = "component", value = "组件", dataType = "String",required = true)
    private String component;

    /**
     * 排序
     */
    @NotNull(message = "排序不能为空")
    @ApiModelProperty(name = "orderNum", value = "排序", dataType = "Integer",required = true)
    private Integer orderNum;

    /**
     * 父id
     */
    @ApiModelProperty(name = "parentId", value = "父id", dataType = "Integer")
    private Integer parentId;

    /**
     * 是否隐藏
     */
    @ApiModelProperty(name = "isHidden", value = "是否隐藏", dataType = "Integer")
    private Integer isHidden;

}