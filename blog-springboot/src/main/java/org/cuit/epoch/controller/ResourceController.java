package org.cuit.epoch.controller;

import io.swagger.annotations.Api;
import io.swagger.annotations.ApiOperation;
import org.cuit.epoch.dto.LabelOptionDTO;
import org.cuit.epoch.dto.resource.ResourceDTO;
import org.cuit.epoch.service.ResourceService;
import org.cuit.epoch.util.Result;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.ResourceVO;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

import javax.validation.Valid;
import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/11/26 14:40
 * @description: 资源(url访问)控制器
 */
@Api(tags = "资源模块")
@RestController
public class ResourceController {
    @Autowired
    private ResourceService resourceService;

    /**
     * 查看资源列表
     *
     * @return {@link Result < ResourceDTO >} 资源列表
     */
    @ApiOperation(value = "查看全部资源列表")
    @GetMapping("/admin/resources")
    public Result<List<ResourceDTO>> listResources(ConditionVO conditionVO) {
        return Result.ok(resourceService.listResources(conditionVO));
    }

    /**
     * 删除资源
     *
     * @param resourceId 资源id
     * @return {@link Result<>}
     */
    @ApiOperation(value = "删除资源")
    @DeleteMapping("/admin/resources/{resourceId}")
    public Result<?> deleteResource(@PathVariable("resourceId") Integer resourceId) {
        resourceService.deleteResource(resourceId);
        return Result.ok();
    }

    /**
     * 新增或修改资源
     *
     * @param resourceVO 资源信息
     * @return {@link Result<>}
     */
    @ApiOperation(value = "新增或修改资源")
    @PostMapping("/admin/resources")
    public Result<?> saveOrUpdateResource(@RequestBody @Valid ResourceVO resourceVO) {
        resourceService.saveOrUpdateResource(resourceVO);
        return Result.ok();
    }

    /**
     * 查看角色资源选项
     *
     * @return {@link Result< LabelOptionDTO >} 角色资源选项
     */
    @ApiOperation(value = "查看角色资源选项")
    @GetMapping("/admin/role/resources")
    public Result<List<LabelOptionDTO>> listResourceOption() {
        return Result.ok(resourceService.listResourceOption());
    }


}