package org.cuit.epoch.service.impl;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.toolkit.StringUtils;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import org.cuit.epoch.dto.RoleDTO;
import org.cuit.epoch.dto.UserRoleDTO;
import org.cuit.epoch.entity.Role;
import org.cuit.epoch.entity.RoleMenu;
import org.cuit.epoch.entity.RoleResource;
import org.cuit.epoch.entity.UserRole;
import org.cuit.epoch.constant.CommonConst;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.handler.MySourceSafilterAuthStrategy;
import org.cuit.epoch.mapper.RoleMapper;
import org.cuit.epoch.mapper.UserRoleMapper;
import org.cuit.epoch.service.RoleMenuService;
import org.cuit.epoch.service.RoleResourceService;
import org.cuit.epoch.service.RoleService;
import org.cuit.epoch.util.BeanCopyUtils;
import org.cuit.epoch.util.PageUtils;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.RoleVO;
import org.cuit.epoch.vo.page.PageResult;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.List;
import java.util.Objects;
import java.util.stream.Collectors;

/**
 * @author: ladidol
 * @date: 2022/11/28 13:18
 * @description:
 */
@Service
public class RoleServiceImpl extends ServiceImpl<RoleMapper, Role> implements RoleService {
    @Autowired
    private RoleMapper roleMapper;
    @Autowired
    private RoleResourceService roleResourceService;
    @Autowired
    private RoleMenuService roleMenuService;
    @Autowired
    private UserRoleMapper userRoleDao;
    @Autowired
    private MySourceSafilterAuthStrategy mySourceSafilterAuthStrategy;


    @Override
    public List<UserRoleDTO> listUserRoles() {
        // 查询角色列表
        List<Role> roleList = roleMapper.selectList(new LambdaQueryWrapper<Role>()
                .select(Role::getId, Role::getRoleName));
        return BeanCopyUtils.copyList(roleList, UserRoleDTO.class);
    }

    @Override
    public PageResult<RoleDTO> listRoles(ConditionVO conditionVO) {
        // 查询角色列表，这里可以直接查询到每一个角色所拥有的资源和菜单列表
        List<RoleDTO> roleDTOList = roleMapper.listRoles(PageUtils.getLimitCurrent(), PageUtils.getSize(), conditionVO);
        // 查询总量
        Integer count = roleMapper.selectCount(new LambdaQueryWrapper<Role>()
                .like(StringUtils.isNotBlank(conditionVO.getKeywords()), Role::getRoleName, conditionVO.getKeywords()));
        return new PageResult<>(roleDTOList, count);
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public void saveOrUpdateRole(RoleVO roleVO) {
        // 判断角色名重复
        Role existRole = roleMapper.selectOne(new LambdaQueryWrapper<Role>()
                .select(Role::getId)
                .eq(Role::getRoleName, roleVO.getRoleName()));
        if (Objects.nonNull(existRole) && !existRole.getId().equals(roleVO.getId())) {
            throw new AppException("角色名已存在");
        }
        // 保存或更新角色信息
        Role role = Role.builder()
                .id(roleVO.getId())
                .roleName(roleVO.getRoleName())
                .roleLabel(roleVO.getRoleLabel())
                .isDisable(CommonConst.FALSE)
                .build();
        this.saveOrUpdate(role);//自动填充roleId
        // 更新角色资源关系
        if (Objects.nonNull(roleVO.getResourceIdList())) {
            if (Objects.nonNull(roleVO.getId())) {
                roleResourceService.remove(new LambdaQueryWrapper<RoleResource>()
                        .eq(RoleResource::getRoleId, roleVO.getId()));
            }
            List<RoleResource> roleResourceList = roleVO.getResourceIdList().stream()
                    .map(resourceId -> RoleResource.builder()
                            .roleId(role.getId())
                            .resourceId(resourceId)
                            .build())
                    .collect(Collectors.toList());
            roleResourceService.saveBatch(roleResourceList);
            // 重新加载角色资源信息
            mySourceSafilterAuthStrategy.clearDataSource();
        }
        // 更新角色菜单关系
        if (Objects.nonNull(roleVO.getMenuIdList())) {
            if (Objects.nonNull(roleVO.getId())) {
                roleMenuService.remove(new LambdaQueryWrapper<RoleMenu>().eq(RoleMenu::getRoleId, roleVO.getId()));
            }
            List<RoleMenu> roleMenuList = roleVO.getMenuIdList().stream()
                    .map(menuId -> RoleMenu.builder()
                            .roleId(role.getId())
                            .menuId(menuId)
                            .build())
                    .collect(Collectors.toList());
            roleMenuService.saveBatch(roleMenuList);
        }
    }

    @Override
    public void deleteRoles(List<Integer> roleIdList) {
        // 判断角色下是否有用户
        Integer count = userRoleDao.selectCount(new LambdaQueryWrapper<UserRole>()
                .in(UserRole::getRoleId, roleIdList));
        if (count > 0) {
            throw new AppException("该角色下存在用户");
        }
        roleMapper.deleteBatchIds(roleIdList);
    }

}