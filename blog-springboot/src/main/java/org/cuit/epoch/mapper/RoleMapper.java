package org.cuit.epoch.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;
import org.cuit.epoch.dto.ResourceRoleDTO;
import org.cuit.epoch.dto.RoleDTO;
import org.cuit.epoch.entity.Role;
import org.cuit.epoch.vo.ConditionVO;

import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/11/15 21:14
 * @description:
 */
@Mapper
public interface RoleMapper extends BaseMapper<Role> {

    /**
     * 查询路由角色列表，获取非匿名访问的路由。
     *
     * @return 角色标签
     */
    List<ResourceRoleDTO> listResourceRoles();

    /**
     * 根据用户id获取角色列表
     *
     * @param userInfoId 用户id
     * @return 角色标签
     */
    List<String> listRolesByUserInfoId(Integer userInfoId);

    /**
     * 查询角色列表
     *
     * @param current     页码
     * @param size        条数
     * @param conditionVO 条件
     * @return 角色列表
     */
    List<RoleDTO> listRoles(@Param("current") Long current, @Param("size") Long size, @Param("conditionVO") ConditionVO conditionVO);


}