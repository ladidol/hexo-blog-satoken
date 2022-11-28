package org.cuit.epoch.controller;

import io.swagger.annotations.Api;
import io.swagger.annotations.ApiOperation;
import org.cuit.epoch.annotation.OptLog;
import org.cuit.epoch.dto.RoleDTO;
import org.cuit.epoch.dto.UserRoleDTO;
import org.cuit.epoch.service.RoleService;
import org.cuit.epoch.util.Result;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.RoleVO;
import org.cuit.epoch.vo.page.PageResult;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

import javax.validation.Valid;
import java.util.List;

import static org.cuit.epoch.enums.OptTypeConst.REMOVE;
import static org.cuit.epoch.enums.OptTypeConst.SAVE_OR_UPDATE;

/**
 * @author: ladidol
 * @date: 2022/11/28 13:13
 * @description:
 */
@Api(tags = "角色模块")
@RestController
public class RoleController {
    @Autowired
    private RoleService roleService;

    /**
     * 查询全部用户角色
     *
     * @return {@link Result < UserRoleDTO >} 用户角色选项
     */
    @ApiOperation(value = "查询全部用户角色")
    @GetMapping("/admin/users/role")
    public Result<List<UserRoleDTO>> listUserRoles() {
        return Result.ok(roleService.listUserRoles());
    }

    /**
     * 查询角色列表，用于角色管理中查询角色的资源管理情况
     *
     * @param conditionVO 条件
     * @return {@link Result< RoleDTO >} 角色列表
     */
    @ApiOperation(value = "分页查询角色列表")
    @GetMapping("/admin/roles")
    public Result<PageResult<RoleDTO>> listRoles(ConditionVO conditionVO) {
        return Result.ok(roleService.listRoles(conditionVO));
    }

    /**
     * 保存或更新角色
     *
     * @param roleVO 角色信息
     * @return {@link Result<>}
     */
    @OptLog(optType = SAVE_OR_UPDATE)
    @ApiOperation(value = "保存或更新角色")
    @PostMapping("/admin/role")
    public Result<?> saveOrUpdateRole(@RequestBody @Valid RoleVO roleVO) {
        roleService.saveOrUpdateRole(roleVO);
        return Result.ok();
    }

    /**
     * 删除角色
     *
     * @param roleIdList 角色id列表
     * @return {@link Result<>}
     */
    @OptLog(optType = REMOVE)
    @ApiOperation(value = "删除角色")
    @DeleteMapping("/admin/roles")
    public Result<?> deleteRoles(@RequestBody List<Integer> roleIdList) {
        roleService.deleteRoles(roleIdList);
        return Result.ok();
    }

}
