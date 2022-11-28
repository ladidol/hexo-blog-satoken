package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.RoleDTO;
import org.cuit.epoch.dto.UserRoleDTO;
import org.cuit.epoch.entity.Role;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.RoleVO;
import org.cuit.epoch.vo.page.PageResult;

import java.util.List;

public interface RoleService extends IService<Role> {

    /**
     * 获取用户角色选项
     *
     * @return 角色
     */
    List<UserRoleDTO> listUserRoles();

    /**
     * 查询角色列表
     *
     * @param conditionVO 条件
     * @return 角色列表
     */
    PageResult<RoleDTO> listRoles(ConditionVO conditionVO);

    /**
     * 保存或更新角色
     *
     * @param roleVO 角色
     */
    void saveOrUpdateRole(RoleVO roleVO);

    /**
     * 删除角色
     * @param roleIdList 角色id列表
     */
    void deleteRoles(List<Integer> roleIdList);

}
